package server

import (
	"math"
	"slices"
	"strconv"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/command"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/plugin"
	"github.com/golem-mc/golem/registry"
)

// Sleeping: vanilla ServerPlayer's startSleepInBed and stopSleepInBed,
// Player.tick's sleep counter, ServerLevel.tickSleeping's night skip once
// enough sleepers have lain 100 ticks, and the players-sleeping message.
// Packet order is vanilla's, since a real client is the only judge of what
// a sleeper sees: leave-bed animation, bed occupancy, the pose and sleeping
// position metadata, then the position the bed puts the body at.

// sleepTicksToSkip is the ticks a sleeper must lie before counting toward
// the skip (vanilla's isSleepingLongEnough).
const sleepTicksToSkip = 100

// sleepTicksAfterWake is where a wake's residue counter resets: vanilla's
// Player.tick counts it up to 110 then clears it, so a body that stood up at
// 100 is not counted at the next sleep's start.
const sleepTicksAfterWake = 110

// sleepingPercentage is the share of players that must sleep to pass the
// night (GameRules.PLAYERS_SLEEPING_PERCENTAGE), read from the sleeping world.
func sleepingPercentage(w *world.World) int {
	return int(w.IntRule(world.RulePlayersSleeping))
}

// sleepersNeeded is vanilla's SleepStatus.sleepersNeeded: the percentage of
// active players, rounded up, at least one, so a lone sleeper passes the
// night.
func sleepersNeeded(active, percentage int) int {
	needed := max(int(math.Ceil(float64(float32(active)*float32(percentage)/100.0))), 1)
	return needed
}

// updateSleepingPlayerList recounts every dimension's sleepers and sends the
// players-sleeping message where it changed. It runs when a sleep starts or
// ends and on join, leave, dimension change or game mode change; the tick
// reads the cached count between.
// vanilla: ServerLevel.updateSleepingPlayerList
func (s *Server) updateSleepingPlayerList() {
	for _, d := range s.allDims() {
		if s.recountSleepers(d) {
			s.announceSleepStatus(d)
		}
	}
}

// recountSleepers counts the dimension's players and sleepers into the
// cache and reports whether the message has news: someone slept before or
// now, and a count changed. An empty dimension is not recounted.
// vanilla: SleepStatus.update, ServerLevel.updateSleepingPlayerList
func (s *Server) recountSleepers(d *dimension) bool {
	s.mu.RLock()
	clients := s.clientsLocked()
	s.mu.RUnlock()

	// Counted under sleepMu so two racing recounts cannot store the older last.
	// Lock order: sleepMu, then the players' locks.
	s.sleepMu.Lock()
	defer s.sleepMu.Unlock()
	active, sleeping, present := 0, 0, 0
	for _, c := range clients {
		if c.keepAlive == nil || c.theWorld() != d.w {
			continue
		}
		present++
		if c.player.GameMode() == protocol.GameModeSpectator {
			continue
		}
		active++
		if c.player.Sleep().Lying {
			sleeping++
		}
	}
	if present == 0 {
		return false
	}

	wasActive, wasSleeping := d.sleepStatus.active, d.sleepStatus.sleeping
	d.sleepStatus = sleepCounts{active: active, sleeping: sleeping}

	if wasSleeping <= 0 && sleeping <= 0 {
		return false
	}
	return wasActive != active || wasSleeping != sleeping
}

// announceSleepStatus sends the dimension's players the sleeping message
// over the hotbar (vanilla's ServerLevel.announceSleepStatus): the night
// will pass when enough sleep, else how many more are needed. A share above
// 100, the rule's way of never passing, sends nothing.
func (s *Server) announceSleepStatus(d *dimension) {
	pct := sleepingPercentage(d.w)
	if pct > 100 {
		return
	}
	s.sleepMu.Lock()
	active, sleeping := d.sleepStatus.active, d.sleepStatus.sleeping
	s.sleepMu.Unlock()

	var message protocol.Text
	if sleeping >= sleepersNeeded(active, pct) {
		message = protocol.Text{Translate: "sleep.skipping_night"}
	} else {
		message = protocol.Text{
			Translate: "sleep.players_sleeping",
			With: []protocol.Text{
				{Text: strconv.Itoa(sleeping)},
				{Text: strconv.Itoa(sleepersNeeded(active, pct))},
			},
		}
	}
	s.broadcastWorld(d.w, protocol.IDPlaySystemChat, systemChatPayload(message, true))
}

// startSleeping lays the body down (the tail of vanilla's
// ServerPlayer.startSleepInBed): the body takes the sleeping state and the
// bed's position; if the share can never be met the sleeper is told by
// overlay; the recount runs, its message on the wire before the bed's news
// (vanilla's order); the bed becomes occupied (flags-3 set, stirring the
// other half); and finally every client, the sleeper's included, gets the
// pose and bed position, which the sleeper's client moves the body into
// the bed by. The pose is derived from the new sleeping state before
// sending, as vanilla's setPose does.
//
// The whole start is one claim under lookMu, the sleeping state and
// occupancy changing together: a wake claiming in between would clear no
// occupancy, leaving an occupied empty bed, and its stand-up baseline would
// be overwritten. Inside, the bed is claimed under s.bedMu, shared by every
// click, so the occupied check and set are one step: of two clicks racing
// for a free bed, the second finds it taken and returns false, as vanilla's
// single thread decides.
func (c *client) startSleeping(w *world.World, x, y, z int32, bed registry.Block, canSetSpawn bool) bool {
	// vanilla: LivingEntity.startSleeping stops riding before moving to bed.
	// The bed supplies the position, so skip the dismount's step and teleport.
	c.dismountBoat(false)
	c.dismountMinecart(false)
	c.dismountCushion(false)

	// Read the share before lookMu and bedMu, so the rule's lock is never held
	// under the sleep's.
	cannotPass := sleepingPercentage(c.theWorld()) > 100
	c.lookMu.Lock()
	c.srv.bedMu.Lock()
	height, ok := rules.BedSleepHeight(w, x, y, z)
	if !ok || bedOccupiedNow(w, x, y, z) {
		c.srv.bedMu.Unlock()
		c.lookMu.Unlock()
		return false
	}
	pos := c.player.Position()
	// vanilla: LivingEntity.setPosToBed offsets the body above the bed's shape.
	pos.X, pos.Y, pos.Z = float64(x)+0.5, float64(y)+height+0.125, float64(z)+0.5
	c.player.SetSleep(player.Sleep{Lying: true, X: x, Y: y, Z: z})
	// vanilla: ServerPlayer.startSleeping
	c.resetStat(player.Custom(registry.CustomStatTimeSinceRest))
	c.player.SetPosition(pos)
	look := c.player.Appearance()
	c.refreshPose(&look)
	c.player.SetAppearance(look)
	// Viewers decode positions relative to the last one they were told and
	// setPosToBed does not move it; a viewer arriving now is spawned at the baseline.
	// vanilla: ServerEntity.sendChanges
	c.tellObserversTeleported()
	// vanilla: ServerPlayer.startSleepInBed, before updateSleepingPlayerList.
	if cannotPass {
		c.send(protocol.IDPlaySystemChat,
			systemChatPayload(protocol.Text{Translate: "sleep.not_possible"}, true))
	}
	c.srv.updateSleepingPlayerList()
	c.srv.setBedOccupied(w, x, y, z, true)
	c.srv.broadcastWorld(w, protocol.IDPlayEntityMetadata, playerMetadata(c.player, true))
	c.srv.bedMu.Unlock()
	c.lookMu.Unlock()
	c.awardStat(player.Custom(rules.SleptInBedStat(bed)), 1)
	// vanilla: ServerPlayer.startSleepInBed fires slept_in_bed only with canSetSpawn.
	if canSetSpawn {
		c.sleptInBedAdvancements()
	}
	return true
}

// stopSleeping stands a sleeper up (vanilla's ServerPlayer
// .stopSleepInBed). In order: the leave-bed animation to everyone,
// sleeper included; the counter is set (0 for a wake that starts afresh,
// the 100 reached for wakes by the sleeper, the day or a blow); the body
// moves to the bed's wake position, facing away from it; the recount (for
// wakes answering a sleep start or the player leaving), its message before
// the wake's news; the position to the sleeper's client; the bed freed
// (flags-3 set, stirring the other half); and the pose and sleeping
// position cleared in metadata. The pose is derived from the ended sleep
// directly into the tracked appearance the packet is built from, not via
// the broadcasting appearance update, since this packet carries it.
//
// The tail (recount, position, bed, metadata) runs within the wake's
// claim, so a bed click held up by it starts its sleep only after every
// wake packet is out; otherwise its lying metadata would precede the
// wake's standing one and clients would show a body the server keeps
// lying.
//
// If the bed vanished, vanilla's own test applies: no bed to clear and no
// wake position, so the body stays put; the position packet still goes,
// resending the same position as vanilla's wake does.
func (c *client) stopSleeping(forceful, updateSleepers bool) {
	c.pluginBedLeave()
	// The counter left: 100 for a wake that completes the sleep, 0 for one that
	// starts afresh.
	ticks := 100
	if forceful {
		ticks = 0
	}
	// Claim the wake in the same step that reads the sleep, the tail running
	// within the claim: two wakes run it once, the tick's counter cannot write
	// the sleep back, and a held-up bed click waits for the wake's last packet.
	c.lookMu.Lock()
	var sl player.Sleep
	c.player.UpdateSleep(func(s *player.Sleep) {
		sl = *s
		if s.Lying {
			*s = player.Sleep{Ticks: ticks}
		}
	})
	if !sl.Lying {
		c.lookMu.Unlock()
		return
	}
	w := c.theWorld()

	// The leave-bed animation (the arm swing vanilla's client ends its wake
	// on), to everyone including the sleeper.
	c.srv.broadcastWorld(w, protocol.IDPlayEntityAnimation,
		entityAnimationPayload(c.player.EntityID, protocol.AnimationLeaveBed))

	x, y, z := sl.X, sl.Y, sl.Z
	pos := c.player.Position()
	state := w.BlockAt(x, y, z)
	if bed := registry.BlockOf(state); bed.InGroup(registry.GroupBed) {
		// A destroy-on-leave bed goes before the stand-up search, which then
		// sees the cells empty. vanilla: LivingEntity.stopSleeping,
		// AbstractBedBlock.onStopSleeping
		if rules.BedRuleOf(w.DimensionTypeName(), bed).DestroyOnLeave && bed == registry.BlockStrawBed {
			c.srv.destroyStrawBed(w, x, y, z)
		}
		// The wake reads the lying yaw and turns the body away from the bed. The
		// occupancy waits for the position packet, vanilla's order.
		if facing := registry.StateValue(state, registry.PropFacing); facing != "" {
			sx, sy, sz, yaw := standUpPosition(w, x, y, z, facing, pos.Yaw)
			pos.X, pos.Y, pos.Z = sx, sy, sz
			pos.Yaw, pos.Pitch = yaw, 0
		}
	}
	// Position and wait in one step, so a move sent before the client saw the
	// wake cannot land between.
	sync := c.placeAt(pos)
	// Viewers decode positions relative to the last one they were told.
	c.tellObserversTeleported()

	// The pose from now: no longer sleeping, what the inputs ask, derived into
	// the appearance the wake's packet is built from.
	look := c.player.Appearance()
	c.refreshPose(&look)
	c.player.SetAppearance(look)

	if updateSleepers {
		c.srv.updateSleepingPlayerList()
	}
	c.send(protocol.IDPlaySyncPosition, sync)
	if registry.BlockOf(w.BlockAt(x, y, z)).InGroup(registry.GroupBed) {
		c.srv.setBedOccupied(w, x, y, z, false)
	}
	c.srv.broadcastWorld(w, protocol.IDPlayEntityMetadata, sleepEndMetadata(c.player))
	c.lookMu.Unlock()
}

// setBedOccupied sets a bed's occupied property as setBlock flags 3 does:
// world, clients, and the neighbour updates.
// vanilla: Level.setBlock
func (s *Server) setBedOccupied(w *world.World, x, y, z int32, occupied bool) {
	state := w.BlockAt(x, y, z)
	if !registry.BlockOf(state).InGroup(registry.GroupBed) {
		return
	}
	if registry.StateBool(state, registry.PropOccupied) == occupied {
		return
	}
	newState, ok := registry.WithBool(state, registry.PropOccupied, occupied)
	if !ok {
		return
	}
	w.SetBlock(x, y, z, newState)
	s.markChanged(w, x, y, z)
	s.blockChanged(w, x, y, z, state)
}

// bedOccupiedNow reads the bed's current occupied property, which the
// claim checks instead of the click's copy; a non-bed reports false.
func bedOccupiedNow(w *world.World, x, y, z int32) bool {
	state := w.BlockAt(x, y, z)
	return registry.BlockOf(state).InGroup(registry.GroupBed) && registry.StateBool(state, registry.PropOccupied)
}

// monsterNearBed is vanilla's Monster.isPreventingPlayerRest: whether any
// mob that prevents this player's rest is within the box around the bed's centre, 16 across and 10
// high, regardless of what it is doing or how it got there.
func (c *client) monsterNearBed(w *world.World, x, y, z int32) bool {
	herd := c.srv.herdOf(w)
	c.srv.mobsMu.Lock()
	defer c.srv.mobsMu.Unlock()

	cx, cy, cz := float64(x)+0.5, float64(y), float64(z)+0.5
	minX, minY, minZ := cx-8, cy-5, cz-8
	maxX, maxY, maxZ := cx+8, cy+5, cz+8
	vitals := c.player.Vitals()
	mode := c.player.GameMode()
	sleeper := &entity.Player{EntityID: c.player.EntityID, UUID: c.player.UUID, Dead: vitals.Dead,
		Huntable: !vitals.Dead && mode != protocol.GameModeCreative && mode != protocol.GameModeSpectator}
	near := func(m *entity.Mob) bool {
		// The check searches the Monster class.
		if !m.PreventsRest(w, sleeper) {
			return false
		}
		bx0, by0, bz0, bx1, by1, bz1 := m.BlastBox()
		return bx0 <= maxX && bx1 >= minX && by0 <= maxY && by1 >= minY && bz0 <= maxZ && bz1 >= minZ
	}
	if slices.ContainsFunc(herd.herd, near) {
		return true
	}
	// A killed mob stays in the level through its death animation.
	for _, corpse := range herd.dying {
		if near(corpse.ent.mob) {
			return true
		}
	}
	return false
}

// tickSleeping is the tick's sleep work (vanilla's
// ServerLevel.tickSleeping): first each dimension's night-pass check (the
// cached share from the last start or end, and a live count of sleepers past
// 100 ticks), then every sleeper's counter and dawn rule. Passing the night
// moves the dimension's default clock to the wake marker, wakes its sleepers,
// and clears the rain they slept through.
func (s *Server) tickSleeping() {
	s.mu.RLock()
	clients := s.clientsLocked()
	s.mu.RUnlock()

	for _, d := range s.allDims() {
		s.passNight(d, clients)
	}

	// A wake's residue counts up to the 110 reset (vanilla's Player.tick) for
	// every non-lying body in every dimension; the per-sleeper pass below
	// handles the rest. The night's wakes above count here the same tick; a
	// wake from the pass below counts from the next tick, since the pass writes
	// its 100 after this loop reads.
	for _, c := range clients {
		if c.keepAlive == nil {
			continue
		}
		c.player.UpdateSleep(func(s *player.Sleep) {
			if !s.Lying && s.Ticks > 0 {
				s.Ticks++
				if s.Ticks >= sleepTicksAfterWake {
					s.Ticks = 0
				}
			}
		})
	}

	// Per-sleeper pass: Player.tick's counter (capped at the 100 the skip
	// counts) and the dawn rule (a sleeper caught by day wakes). A bed gone
	// under its sleeper wakes them first (vanilla LivingEntity.tick's
	// checkBedExists), as vanilla's stopSleeping does: silently, the counter
	// discarded.
	for _, c := range clients {
		if c.keepAlive == nil {
			continue
		}
		w := c.theWorld()
		sl := c.player.Sleep()
		if !sl.Lying {
			continue
		}
		if _, ok := rules.BedSleepHeight(w, sl.X, sl.Y, sl.Z); !ok {
			c.stopSleeping(true, true)
			continue
		}
		c.player.UpdateSleep(func(s *player.Sleep) {
			if s.Lying && s.Ticks < sleepTicksToSkip {
				s.Ticks++
			}
		})
		if w.BrightOutside() {
			c.stopSleeping(false, true)
		}
	}
}

// passNight is the dimension's skip check: the cached share says whether
// there are enough sleepers, and those past 100 ticks are counted live.
// vanilla: ServerLevel.tick
func (s *Server) passNight(d *dimension, clients []*client) {
	w := d.w
	pct := sleepingPercentage(w)
	s.sleepMu.Lock()
	enough := d.sleepStatus.sleeping >= sleepersNeeded(d.sleepStatus.active, pct)
	s.sleepMu.Unlock()
	if !enough {
		return
	}
	deep := 0
	for _, c := range clients {
		if c.theWorld() != w {
			continue
		}
		if c.player.GameMode() == protocol.GameModeSpectator {
			continue
		}
		if sl := c.player.Sleep(); sl.Lying && sl.Ticks >= sleepTicksToSkip {
			deep++
		}
	}
	s.sleepMu.Lock()
	needed := sleepersNeeded(d.sleepStatus.active, pct)
	s.sleepMu.Unlock()
	if deep < needed {
		return
	}
	// The wake marker is a clock move, gated by advance_time and by the
	// dimension type having a default clock; the waking and weather reset are
	// not. vanilla: ServerLevel.tick, moveToTimeMarker(WAKE_UP_FROM_SLEEP)
	if clock, ok := w.DefaultClock(); ok && w.BoolRule(world.RuleAdvanceTime) {
		// Send the jump at once, before the wakes, in vanilla's order (the clock
		// broadcast, then the wakes' packets).
		s.skipClock(w, plugin.SkipNight, func() bool { w.WakeUpFromSleep(clock); return true })
		s.broadcastLevel(w, protocol.IDPlayUpdateTime, updateTimePayload(w, clock))
	}
	s.wakeUpAllPlayers(d, clients)
	// vanilla: resetWeatherCycle behind ADVANCE_WEATHER and isRaining.
	if w.BoolRule(world.RuleAdvanceWeather) && w.IsRaining() && s.resetWeatherAllowed(d) {
		w.ResetWeatherCycle()
	}
}

// wakeUpAllPlayers wakes every sleeper in the world (vanilla's
// ServerLevel.wakeUpAllPlayers): the cached count empties and the wakes
// run, announcing nothing since the message already said the night would
// pass.
func (s *Server) wakeUpAllPlayers(d *dimension, clients []*client) {
	s.sleepMu.Lock()
	d.sleepStatus.sleeping = 0
	s.sleepMu.Unlock()
	for _, c := range clients {
		if c.theWorld() == d.w && c.player.Sleep().Lying {
			// The night passing does not restart the sleep: the residue is 100,
			// vanilla's stopSleepInBed(false, false).
			c.stopSleeping(false, false)
		}
	}
}

// standUpPosition finds a bed exit and turns the waking player toward the bed.
// vanilla: LivingEntity.stopSleeping
func standUpPosition(w *world.World, x, y, z int32, facing string, yaw float32) (sx, sy, sz float64, wyaw float32) {
	fd, known := facingDelta[facing]
	if !known {
		fd = facingDelta["south"]
	}
	if sx, sy, sz, ok := rules.BedStandUpSpot(w, x, y, z, [2]int32{fd[0], fd[1]}, yaw); ok {
		return sx, sy, sz, wakeYaw(x, z, sx, sz)
	}
	sx, sy, sz = float64(x)+0.5, float64(y)+1.1, float64(z)+0.5
	return sx, sy, sz, wakeYaw(x, z, sx, sz)
}

// wakeYaw is the waking body's yaw facing the bed: vanilla's atan2 toward
// the bed minus the body yaw's quarter turn, wrapped to the half circle. A
// body standing on the bed's centre faces east, vanilla's atan2(0, 0).
func wakeYaw(bx, bz int32, sx, sz float64) float32 {
	dx, dz := float64(bx)+0.5-sx, float64(bz)+0.5-sz
	if dx == 0 && dz == 0 {
		return -90
	}
	return command.WrapDegrees(float32(math.Atan2(dz, dx)*180/math.Pi - 90))
}
