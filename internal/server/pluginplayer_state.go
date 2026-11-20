package server

import (
	"math"
	"strings"
	"time"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/command"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/plugin"
	"github.com/golem-mc/golem/registry"
)

// pluginAbilities is what a plugin changed in a player's abilities. mayFly is
// 0 to follow the game mode, 1 to grant flight and -1 to revoke it; a game
// mode change resets it. vanilla: Abilities.mayfly, flyingSpeed, walkingSpeed
type pluginAbilities struct {
	mayFly              int8
	flySpeed, walkSpeed float32
}

func (c *client) abilityState() pluginAbilities {
	if a := c.abilities.Load(); a != nil {
		return *a
	}
	return pluginAbilities{flySpeed: 0.05, walkSpeed: 0.1}
}

func (c *client) updateAbilities(change func(*pluginAbilities)) {
	for {
		old := c.abilities.Load()
		next := c.abilityState()
		change(&next)
		if c.abilities.CompareAndSwap(old, &next) {
			return
		}
	}
}

// resetFlightGrant drops a plugin's flight grant on a game mode change.
// vanilla: ServerPlayer.setGameMode, GameType.updatePlayerAbilities
func (c *client) resetFlightGrant() {
	if c.abilities.Load() != nil {
		c.updateAbilities(func(a *pluginAbilities) { a.mayFly = 0 })
	}
}

// hides reports whether this player is not shown the entity with the ID.
func (c *client) hides(id int32) bool {
	m := c.hidden.Load()
	if m == nil {
		return false
	}
	_, ok := (*m)[id]
	return ok
}

// setHidden changes whether the entity is hidden from this player and reports
// whether that was a change.
func (c *client) setHidden(id int32, hide bool) bool {
	for {
		old := c.hidden.Load()
		if c.hides(id) == hide {
			return false
		}
		next := map[int32]struct{}{}
		if old != nil {
			for k := range *old {
				next[k] = struct{}{}
			}
		}
		if hide {
			next[id] = struct{}{}
		} else {
			delete(next, id)
		}
		var store *map[int32]struct{}
		if len(next) > 0 {
			store = &next
		}
		if c.hidden.CompareAndSwap(old, store) {
			return true
		}
	}
}

func (p pluginPlayer) GameMode() plugin.GameMode {
	if !p.IsValid() {
		return plugin.Survival
	}
	return plugin.GameMode(p.c.player.GameMode())
}

func (p pluginPlayer) SetGameMode(mode plugin.GameMode) {
	if p.IsValid() && mode <= plugin.Spectator {
		p.c.SetGameMode(int32(mode))
	}
}

// SetOpLevel changes the operator list entry as /op and /deop do.
func (p pluginPlayer) SetOpLevel(level int) {
	if !p.IsValid() {
		return
	}
	s, id := p.c.srv, p.c.player.UUID
	if level <= 0 {
		s.deop(id)
	} else {
		s.opPut(opEntry{UUID: id.String(), Name: p.c.profile.Name, Level: min(level, 4)})
	}
	p.c.SetPermLevel(s.ops.Level(id))
	p.c.ResendPermissionState()
}

// Chat says msg as the player; a slash makes it a command.
func (p pluginPlayer) Chat(msg string) {
	if !p.IsValid() {
		return
	}
	if rest, ok := strings.CutPrefix(msg, "/"); ok {
		p.RunCommand(rest)
		return
	}
	if _, bad := illegalChatCharacter(msg); bad || strings.TrimSpace(msg) == "" {
		return
	}
	p.c.srv.pluginChat(p.c, msg, nil)
}

func (p pluginPlayer) Cooldown(group plugin.Key) int {
	if !p.IsValid() {
		return 0
	}
	p.c.shieldMu.Lock()
	defer p.c.shieldMu.Unlock()
	return int(p.c.cooldowns[group.String()])
}

// SetCooldown starts a cooldown group, or ends it with ticks of 0 or less.
// vanilla: ItemCooldowns.addCooldown, removeCooldown
func (p pluginPlayer) SetCooldown(group plugin.Key, ticks int) {
	if !p.IsValid() {
		return
	}
	name := group.String()
	if ticks > 0 {
		p.c.startItemCooldown(name, int32(min(ticks, 1<<30)))
		return
	}
	p.c.shieldMu.Lock()
	_, had := p.c.cooldowns[name]
	delete(p.c.cooldowns, name)
	p.c.shieldMu.Unlock()
	if had {
		p.c.send(protocol.IDPlayCooldown, newCooldownPayload(name, 0))
	}
}

func (p pluginPlayer) vitals() (v player.Vitals) {
	if p.IsValid() {
		v = p.c.player.Vitals()
	}
	return v
}

func (p pluginPlayer) setVitals(change func(*player.Vitals)) {
	if !p.IsValid() {
		return
	}
	p.c.player.UpdateVitals(change)
	p.c.sendHealth(p.c.player.Vitals())
}

func (p pluginPlayer) Food() int           { return int(p.vitals().Food) }
func (p pluginPlayer) Saturation() float32 { return float32(p.vitals().Saturation) }
func (p pluginPlayer) Exhaustion() float32 { return float32(p.vitals().Exhaustion) }

func (p pluginPlayer) SetFood(n int) {
	p.setVitals(func(v *player.Vitals) { v.Food = int32(min(max(n, 0), 20)) })
}
func (p pluginPlayer) SetSaturation(f float32) {
	p.setVitals(func(v *player.Vitals) { v.Saturation = float64(min(max(f, 0), 20)) })
}
func (p pluginPlayer) SetExhaustion(f float32) {
	p.setVitals(func(v *player.Vitals) { v.Exhaustion = float64(min(max(f, 0), 40)) })
}

func (p pluginPlayer) Level() int           { return p.vitals().ExperienceLevel }
func (p pluginPlayer) Progress() float32    { return p.vitals().ExperienceProgress }
func (p pluginPlayer) TotalExperience() int { return p.vitals().ExperienceTotal }

func (p pluginPlayer) SetLevel(n int) {
	if p.IsValid() {
		p.c.SetExperienceLevels(max(n, 0))
	}
}

func (p pluginPlayer) SetProgress(f float32) {
	if !p.IsValid() {
		return
	}
	p.c.player.UpdateVitals(func(v *player.Vitals) { v.ExperienceProgress = min(max(f, 0), 1) })
	p.c.send(protocol.IDPlaySetExperience, setExperiencePayload(p.c.player.Vitals()))
}

func (p pluginPlayer) GiveExperience(points int) {
	if p.IsValid() {
		p.c.GiveExperiencePoints(points)
	}
}

func (p pluginPlayer) Flying() bool { return p.IsValid() && p.c.flying.Load() }

// SetFlying starts or ends flight; a player who may not fly stays on the ground.
func (p pluginPlayer) SetFlying(fly bool) {
	if !p.IsValid() || fly && p.c.abilityFlags()&protocol.AbilityAllowFlying == 0 {
		return
	}
	if p.c.player.GameMode() == protocol.GameModeSpectator {
		return
	}
	if p.c.flying.Swap(fly) != fly {
		p.c.updateSwimming()
		p.c.send(protocol.IDPlayAbilities, p.c.abilitiesPayload())
	}
}

func (p pluginPlayer) AllowFlight() bool {
	return p.IsValid() && p.c.abilityFlags()&protocol.AbilityAllowFlying != 0
}

func (p pluginPlayer) SetAllowFlight(allow bool) {
	if !p.IsValid() || p.c.player.GameMode() == protocol.GameModeSpectator {
		return
	}
	p.c.updateAbilities(func(a *pluginAbilities) {
		a.mayFly = -1
		if allow {
			a.mayFly = 1
		}
	})
	if !allow {
		p.c.flying.Store(false)
		p.c.updateSwimming()
	}
	p.c.send(protocol.IDPlayAbilities, p.c.abilitiesPayload())
}

// FlySpeed and WalkSpeed answer on Bukkit's scale, twice vanilla's ability
// values. vanilla-bukkit: CraftPlayer.getFlySpeed
func (p pluginPlayer) FlySpeed() float32 {
	if !p.IsValid() {
		return 0
	}
	return p.c.abilityState().flySpeed * 2
}

// SetFlySpeed takes Bukkit's scale, clamped to -1..1, and stores half of it.
// vanilla-bukkit: CraftPlayer.setFlySpeed
func (p pluginPlayer) SetFlySpeed(v float32) {
	if p.IsValid() {
		p.c.updateAbilities(func(a *pluginAbilities) { a.flySpeed = min(max(v, -1), 1) / 2 })
		p.c.send(protocol.IDPlayAbilities, p.c.abilitiesPayload())
	}
}

func (p pluginPlayer) WalkSpeed() float32 {
	if !p.IsValid() {
		return 0
	}
	return p.c.abilityState().walkSpeed * 2
}

// SetWalkSpeed takes Bukkit's scale, clamped to -1..1, and sets half of it as
// the walking speed the client draws its field of view by and as the movement
// speed attribute's base. vanilla-bukkit: CraftPlayer.setWalkSpeed
func (p pluginPlayer) SetWalkSpeed(v float32) {
	if !p.IsValid() {
		return
	}
	v = min(max(v, -1), 1) / 2
	p.c.updateAbilities(func(a *pluginAbilities) { a.walkSpeed = v })
	p.c.player.Attributes(func(a *player.Attributes) { a.SetBase(registry.AttributeMovementSpeed, float64(v)) })
	p.c.send(protocol.IDPlayAbilities, p.c.abilitiesPayload())
}

func (p pluginPlayer) Sneaking() bool  { return p.IsValid() && p.c.player.Appearance().Sneaking }
func (p pluginPlayer) Sprinting() bool { return p.IsValid() && p.c.player.Appearance().Sprinting }

// SetRespawnPoint sets where the player respawns; nil returns them to the
// world spawn. vanilla: ServerPlayer.setRespawnPosition
func (p pluginPlayer) SetRespawnPoint(loc *plugin.Location, forced bool) {
	if !p.IsValid() {
		return
	}
	if loc == nil {
		p.c.spawnPoint.Store(nil)
		return
	}
	w, ok := loc.World.(pluginWorld)
	if !ok || w.dim == nil {
		return
	}
	pos := world.SpawnPos{X: int32(math.Floor(loc.Pos.X)), Y: int32(math.Floor(loc.Pos.Y)), Z: int32(math.Floor(loc.Pos.Z)), Yaw: loc.Yaw, Pitch: loc.Pitch}
	p.c.setRespawn(pos, loc.Yaw, loc.Pitch, w.dim.name, forced)
}

// CanSee reports whether other is shown to the player.
func (p pluginPlayer) CanSee(other plugin.Entity) bool {
	if !p.IsValid() {
		return false
	}
	target, ok := p.hostOrAdapter().entityOfHandle(other)
	return ok && !p.c.hides(target.EntityID())
}

// Hide takes the entity out of this player's world, and a player out of their
// tab list. The tracker withholds it from then on.
// vanilla: ServerPlayer.hidePlayer's counterpart in ChunkMap.TrackedEntity.updatePlayer
func (p pluginPlayer) Hide(other plugin.Entity) {
	if !p.IsValid() {
		return
	}
	target, ok := p.hostOrAdapter().entityOfHandle(other)
	if !ok || target == command.Entity(p.c) || !p.c.setHidden(target.EntityID(), true) {
		return
	}
	hidden, isPlayer := target.(*client)
	if !isPlayer {
		return
	}
	hidden.trackMu.Lock()
	held := hidden.seenBy[p.c.player.UUID] == p.c
	delete(hidden.seenBy, p.c.player.UUID)
	hidden.trackMu.Unlock()
	if held {
		p.c.sendIn(hidden.theWorld(), protocol.IDPlayRemoveEntities, removeEntity(hidden.player))
	}
	p.c.send(protocol.IDPlayPlayerInfoRemove, playerInfoRemove(hidden.player))
}

// Show undoes Hide: a player is listed again at once and drawn when the next
// tracking pass pairs them.
func (p pluginPlayer) Show(other plugin.Entity) {
	if !p.IsValid() {
		return
	}
	target, ok := p.hostOrAdapter().entityOfHandle(other)
	if !ok || !p.c.setHidden(target.EntityID(), false) {
		return
	}
	if shown, isPlayer := target.(*client); isPlayer {
		p.c.send(protocol.IDPlayPlayerInfoUpdate, playerInfo(playerInfoEntry{player: shown.player}))
	}
}

// Advancements answers the player's progress in a; its changes are sent and
// announced as /advancement does.
func (p pluginPlayer) Advancements(a plugin.Advancement) plugin.AdvancementProgress {
	adv, ok := a.(pluginAdvancement)
	if !ok || !p.IsValid() {
		return nil
	}
	return pluginProgress{c: p.c, node: adv}
}

func (p pluginPlayer) Statistic(s plugin.Statistic) int {
	if !p.IsValid() {
		return 0
	}
	return int(p.c.statValue(player.Stat{Type: s.Type, Key: s.Value}))
}

// SetStatistic sets a counter, moving scoreboard objectives that track it by
// the difference. vanilla: ServerStatsCounter.setValue
func (p pluginPlayer) SetStatistic(s plugin.Statistic, v int) {
	if !p.IsValid() {
		return
	}
	stat := player.Stat{Type: s.Type, Key: s.Value}
	v = min(max(v, 0), 1<<31-1)
	if v == 0 {
		p.c.resetStat(stat)
		return
	}
	p.c.awardStat(stat, int32(v)-p.c.statValue(stat))
}

// Spectating answers the entity the player's view follows.
func (p pluginPlayer) Spectating() (plugin.Entity, bool) {
	if !p.IsValid() {
		return nil, false
	}
	f := p.c.camera.Load()
	if f == nil {
		return nil, false
	}
	target, ok := f.view.(command.Entity)
	if !ok {
		return nil, false
	}
	return p.hostOrAdapter().pluginEntityOf(target), true
}

// SetSpectating points a spectator's view at the entity; nil returns it to
// their own eyes. Anyone else is left alone, as vanilla's /spectate refuses.
// vanilla: ServerPlayer.setCamera
func (p pluginPlayer) SetSpectating(e plugin.Entity) {
	if !p.IsValid() {
		return
	}
	if e == nil {
		p.c.clearCamera()
		return
	}
	if p.c.player.GameMode() != protocol.GameModeSpectator {
		return
	}
	if target, ok := p.hostOrAdapter().entityOfHandle(e); ok {
		p.c.SetCamera(target)
	}
}

// pluginProgress is one player's progress in one advancement.
type pluginProgress struct {
	c    *client
	node pluginAdvancement
}

func (a pluginProgress) done() bool {
	pr := a.c.Advancements()
	return pr != nil && pr.Done(a.node.node)
}

func (a pluginProgress) Done() bool { return a.done() }

func (a pluginProgress) Awarded() []string {
	if pr := a.c.Advancements(); pr != nil {
		return pr.Completed(a.node.node)
	}
	return nil
}

func (a pluginProgress) Remaining() []string {
	if pr := a.c.Advancements(); pr != nil {
		return pr.Remaining(a.node.node)
	}
	return nil
}

func (a pluginProgress) Award(criterion string) bool {
	pr := a.c.Advancements()
	if pr == nil || !pr.Award(a.node.node, criterion, time.Now()) {
		return false
	}
	a.c.AnnounceAdvancements()
	return true
}

func (a pluginProgress) Revoke(criterion string) bool {
	pr := a.c.Advancements()
	return pr != nil && pr.Revoke(a.node.node, criterion)
}

// SetAwarded replaces the obtained criteria without a toast, chat line,
// reward or plugin event; the client is sent the change on the next flush.
func (a pluginProgress) SetAwarded(criteria []string) {
	pr := a.c.Advancements()
	if pr == nil {
		return
	}
	// Held so a tick's flush cannot send the change with show advancements
	// set; earlier pending changes go out first, with the tick's flag.
	// vanilla: PlayerAdvancements.flushDirty
	defer pr.Hold()()
	a.c.FlushAdvancements(true)
	pr.SetAwarded(a.node.node, criteria, time.Now())
	a.c.FlushAdvancements(false)
}

// compassTarget is a spawn position a plugin pointed one player's compasses at.
type compassTarget struct {
	dim string
	pos world.SpawnPos
}

func (p pluginPlayer) CompassTarget() plugin.Location {
	if !p.IsValid() {
		return plugin.Location{}
	}
	if t := p.c.compassTarget.Load(); t != nil {
		w := p.host.pluginWorldByName(t.dim)
		return plugin.Location{World: w, Pos: plugin.Vec3{X: float64(t.pos.X), Y: float64(t.pos.Y), Z: float64(t.pos.Z)}, Yaw: t.pos.Yaw, Pitch: t.pos.Pitch}
	}
	return p.host.pluginWorldByName(p.c.srv.world.SpawnDimension()).SpawnPoint()
}

// SetCompassTarget sends a Set Default Spawn Position the world spawn does
// not follow; nil sends the world spawn again.
func (p pluginPlayer) SetCompassTarget(loc *plugin.Location) {
	if !p.IsValid() {
		return
	}
	var t *compassTarget
	if loc != nil {
		w, ok := loc.World.(pluginWorld)
		if !ok {
			return
		}
		b := loc.Pos.Block()
		t = &compassTarget{dim: w.dim.name, pos: world.SpawnPos{X: int32(b.X), Y: int32(b.Y), Z: int32(b.Z), Yaw: loc.Yaw, Pitch: loc.Pitch}}
	}
	p.c.compassTarget.Store(t)
	if err := p.c.sendSpawnPosition(); err != nil {
		p.c.log.Debug("could not send spawn position", "error", err)
	}
}

func (p pluginPlayer) FallDistance() float64 { return p.vitals().FallDistance }

func (p pluginPlayer) SetFallDistance(blocks float64) {
	if p.IsValid() && !math.IsNaN(blocks) {
		p.c.player.UpdateVitals(func(v *player.Vitals) { v.FallDistance = max(blocks, 0) })
	}
}

func (p pluginPlayer) SetTotalExperience(points int) {
	if !p.IsValid() {
		return
	}
	p.c.giveExperience(func(v *player.Vitals) *rules.LevelUp {
		v.ExperienceLevel, v.ExperienceProgress, v.ExperienceTotal, v.Score = 0, 0, 0, 0
		up := rules.GiveExperiencePoints(v, max(points, 0))
		// Float progress lands a point short of a level boundary; the level
		// and remainder are exact in integers.
		level, rest := 0, max(points, 0)
		for rest >= rules.XpNeededForNextLevel(level) {
			rest -= rules.XpNeededForNextLevel(level)
			level++
		}
		v.ExperienceLevel = level
		v.ExperienceProgress = float32(rest) / float32(rules.XpNeededForNextLevel(level))
		return up
	})
}

func (p pluginPlayer) Experience() int {
	v := p.vitals()
	return int(math.Round(float64(v.ExperienceProgress) * float64(rules.XpNeededForNextLevel(v.ExperienceLevel))))
}

func (p pluginPlayer) SetExperience(points int) bool {
	return p.IsValid() && p.c.SetExperiencePoints(points)
}

// TargetBlock answers the first block outline within max blocks of the eyes.
// vanilla: Level.clip
func (p pluginPlayer) TargetBlock(max int) (plugin.Block, bool) {
	return pluginEntity{host: p.host, id: p.c.player.UUID}.TargetBlock(max)
}

func (e pluginEntity) TargetBlock(max int) (plugin.Block, bool) {
	from, to, ok := e.eyeRay(float64(max))
	b, _ := e.live()
	if !ok || max <= 0 {
		return plugin.Block{}, false
	}
	dim := e.host.srv.dimensionByName(b.DimensionName())
	hit, found := dim.w.ClipBlocks(from.X, from.Y, from.Z, to.X-from.X, to.Y-from.Y, to.Z-from.Z, nil)
	if !found {
		return plugin.Block{}, false
	}
	return plugin.Block{World: e.host.pluginWorldOf(b), Pos: plugin.BlockPos{X: int(hit.X), Y: int(hit.Y), Z: int(hit.Z)}}, true
}

func (p pluginPlayer) LineOfSight(max int, passes func(plugin.BlockState) bool) []plugin.Block {
	return pluginEntity{host: p.host, id: p.c.player.UUID}.LineOfSight(max, passes)
}

func (p pluginPlayer) TargetBlockPassing(max int, passes func(plugin.BlockState) bool) plugin.Block {
	return pluginEntity{host: p.host, id: p.c.player.UUID}.TargetBlockPassing(max, passes)
}
