package server

import (
	"cmp"
	"math"
	"slices"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/scoreboard"
	"github.com/golem-mc/golem/registry"
)

// awardStat adds n to a statistic; before the join's restore there is no
// counter and nothing counts. vanilla: ServerPlayer.awardStat
func (c *client) awardStat(stat player.Stat, n int32) {
	if stats := c.stats.Load(); stats != nil {
		stats.Award(stat, n)
		c.updateStatScore(c.srv.scoreboard, stat, n, false)
		for _, board := range c.srv.privateScoreboards() {
			c.updateStatScore(board, stat, n, false)
		}
	}
}

// awardItemStat adds n to an item statistic (used, broken, picked_up,
// dropped); an unnamed item counts nothing.
func (c *client) awardItemStat(statType registry.StatType, itemID int32, n int32) {
	if _, ok := registry.ItemName(itemID); ok {
		c.awardStat(player.Stat{Type: statType, Key: itemID}, n)
	}
}

// awardOpenStat counts opening the container block once its window was
// requested; a locked one counts too.
func (c *client) awardOpenStat(b registry.Block) {
	if key := rules.OpenStat(b); key != registry.NoCustomStat {
		c.awardStat(player.Custom(key), 1)
	}
}

// statValue is a statistic's count, 0 before the join's restore.
func (c *client) statValue(stat player.Stat) int32 {
	if stats := c.stats.Load(); stats != nil {
		return stats.Value(stat)
	}
	return 0
}

// resetStat sets one of the player's statistics back to zero.
// vanilla: ServerPlayer.resetStat
func (c *client) resetStat(stat player.Stat) {
	if stats := c.stats.Load(); stats != nil {
		stats.Reset(stat)
		c.updateStatScore(c.srv.scoreboard, stat, 0, true)
		for _, board := range c.srv.privateScoreboards() {
			c.updateStatScore(board, stat, 0, true)
		}
	}
}

// tickStats counts a tick of play time (played, in world, alive, sneaking,
// awake) and vehicle distance. vanilla: ServerPlayer.doTick, rideTick
func (c *client) tickStats() {
	c.awardStat(player.Custom(registry.CustomStatPlayTime), 1)
	c.awardStat(player.Custom(registry.CustomStatTotalWorldTime), 1)
	if !c.player.Vitals().Dead {
		c.awardStat(player.Custom(registry.CustomStatTimeSinceDeath), 1)
	}
	if c.player.Appearance().Sneaking {
		c.awardStat(player.Custom(registry.CustomStatSneakTime), 1)
	}
	if !c.player.Sleep().Lying {
		c.awardStat(player.Custom(registry.CustomStatTimeSinceRest), 1)
	}
	c.countRiding()
}

// countRiding counts the vehicle's distance since last tick. The first tick
// aboard only records the seat as the start, so boarding counts nothing.
// vanilla: ServerPlayer.checkRidingStatistics
func (c *client) countRiding() {
	vehicle := c.vehicleKind()
	if vehicle == "" {
		c.ridePrevSet = false
		return
	}
	now := c.player.Position()
	was, started := c.ridePrev, c.ridePrevSet
	c.ridePrev, c.ridePrevSet = now, true
	if !started {
		return
	}
	if stat, cm := rules.RidingStat(now.X-was.X, now.Y-was.Y, now.Z-was.Z, vehicle); stat != registry.NoCustomStat {
		c.awardStat(player.Custom(stat), cm)
	}
}

// countMovement counts a player's own step: any jump it started with and
// the distance. vanilla: ServerGamePacketListenerImpl.handleMovePlayer's jumpFromGround,
// ServerPlayer.checkMovementStatistics
func (c *client) countMovement(was, now player.Position, look player.Appearance, water rules.Water) {
	dy := now.Y - was.Y
	if rules.JumpedFromGround(was.OnGround, now.OnGround, dy) {
		c.awardStat(player.Custom(registry.CustomStatJump), 1)
	}
	stat, cm := rules.MovementStat(now.X-was.X, dy, now.Z-was.Z, rules.Mover{
		Swimming:   look.Swimming,
		Water:      water,
		Climbing:   c.climbingAt(now, look),
		OnGround:   now.OnGround,
		Sprinting:  look.Sprinting,
		Crouching:  look.Pose == protocol.PoseSneaking,
		FallFlying: look.FallFlying,
	})
	if stat != registry.NoCustomStat {
		c.awardStat(player.Custom(stat), cm)
	}
}

// vehicleKind names the ridden vehicle for rules.RidingStat, "" on foot.
func (c *client) vehicleKind() string {
	switch {
	case c.ridingMinecart():
		return "minecart"
	case c.ridingBoat():
		return "boat"
	}
	if ent := c.mount.Load(); ent != nil {
		return ent.mob.Type.Name()
	}
	return ""
}

// countFall counts a landing's fall_one_cm (the fall the landed block
// passes on; a bed halves it) for a player who cannot fly.
// vanilla: Player.causeFallDamage
func (c *client) countFall(fall float64, ground uint16) {
	if fall <= 0 || c.abilityFlags()&protocol.AbilityAllowFlying != 0 {
		return
	}
	effect := rules.FallOn(ground)
	if cm := rules.FallStat(fall*effect.Scale + effect.Offset); cm > 0 {
		c.awardStat(player.Custom(registry.CustomStatFallOneCm), cm)
	}
}

// sendStats answers Request Stats with counters changed since the last
// answer. vanilla: ServerStatsCounter.sendStats
func (c *client) sendStats() {
	stats := c.stats.Load()
	if stats == nil {
		return
	}
	c.send(protocol.IDPlayAwardStats, awardStatsPayload(stats.TakeDirty()))
}

// awardStatsPayload builds Award Statistics: per counter its type ID, key
// ID in that type's registry, and value.
func awardStatsPayload(values map[player.Stat]int32) []byte {
	type entry struct{ typeID, keyID, value int32 }
	entries := make([]entry, 0, len(values))
	for stat, n := range values {
		entries = append(entries, entry{int32(stat.Type), stat.Key, n})
	}
	// The order is free on the wire; sorted, one state packs one way.
	slices.SortFunc(entries, func(a, b entry) int {
		return cmp.Or(cmp.Compare(a.typeID, b.typeID), cmp.Compare(a.keyID, b.keyID))
	})
	var b protocol.Buffer
	b.PutVarInt(int32(len(entries)))
	for _, e := range entries {
		b.PutVarInt(e.typeID)
		b.PutVarInt(e.keyID)
		b.PutVarInt(e.value)
	}
	return b.Bytes()
}

// climbingAt is whether the body at pos climbs, for the climb statistic.
func (c *client) climbingAt(pos player.Position, look player.Appearance) bool {
	w := c.theWorld()
	x, z := int32(math.Floor(pos.X)), int32(math.Floor(pos.Z))
	y := int32(math.Floor(pos.Y))
	return rules.Climbing(w.BlockAt(x, y, z), w.BlockAt(x, y-1, z),
		c.player.GameMode() == protocol.GameModeSpectator, look.FallFlying)
}

func (c *client) updateStatScore(board *scoreboard.Board, stat player.Stat, n int32, reset bool) {
	if !board.CountsStats() {
		return
	}
	if typ, key, ok := stat.Names(); ok && board.CountsStat(typ, key) {
		board.UpdateStat(typ, key, c.scoreHolder(), func(v int32) int32 {
			if reset {
				return 0
			}
			return v + n
		})
	}
}
