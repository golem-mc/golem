package server

import (
	"math/rand"

	"github.com/google/uuid"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/effect"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// The sculk shrieker's server half: players' warden warning trackers, the
// reply, summon and darkness; the block is world/shrieker.go.
// vanilla: SculkShriekerBlockEntity, WardenSpawnTracker

const (
	// vanilla: SculkShriekerBlockEntity.WARNING_SOUND_RADIUS
	shriekWarningRadius = 10
	// vanilla: SculkShriekerBlockEntity.DARKNESS_RADIUS
	shriekDarknessRadius = 40
	// vanilla: WardenSpawnTracker.PLAYER_SEARCH_RADIUS
	wardenSearchRadius = 16
)

// shriekWarningSounds is the answer by warning level.
// vanilla: SculkShriekerBlockEntity.SOUND_BY_LEVEL
var shriekWarningSounds = map[int]registry.Sound{
	1: registry.SoundEntityWardenNearbyClose,
	2: registry.SoundEntityWardenNearbyCloser,
	3: registry.SoundEntityWardenNearbyClosest,
	4: registry.SoundEntityWardenListeningAngry,
}

// clientInWorld is w's player with the UUID, nil for none.
func (s *Server) clientInWorld(w *world.World, id uuid.UUID) *client {
	if c := s.clientByUUID(id); c != nil && c.theWorld() == w {
		return c
	}
	return nil
}

// tickWardenSpawn runs the player's warning timers.
// vanilla: ServerPlayer.tick
func (c *client) tickWardenSpawn() {
	c.srv.wardenMu.Lock()
	c.wardenTracker.Tick()
	c.srv.wardenMu.Unlock()
}

// stepOnShrieker has a body standing on a shrieker try to shriek.
// vanilla: SculkShriekerBlock.stepOn, Entity.applyEffectsFromBlocks
func (c *client) stepOnShrieker(pos player.Position, around rules.Habitat) {
	if !around.OnGround || registry.BlockOf(around.Ground) != registry.BlockSculkShrieker {
		return
	}
	dims := c.dims(c.player.Appearance().Pose)
	x, y, z, _ := rules.OnPosCell(c.theWorld(), pos.X, pos.Y, pos.Z, dims.Width/2)
	c.srv.tryShriek(c.theWorld(), x, y, z, c)
}

// stepOnSculkSensor has a body on a sculk sensor schedule its step, even when
// sneaking and every tick it stands there.
// vanilla: SculkSensorBlock.stepOn, Entity.applyEffectsFromBlocks
func (c *client) stepOnSculkSensor(pos player.Position, around rules.Habitat) {
	if c.player.GameMode() == protocol.GameModeSpectator || !around.OnGround {
		return
	}
	if b := registry.BlockOf(around.Ground); b != registry.BlockSculkSensor && b != registry.BlockCalibratedSculkSensor {
		return
	}
	dims := c.dims(c.player.Appearance().Pose)
	x, y, z, _ := rules.OnPosCell(c.theWorld(), pos.X, pos.Y, pos.Z, dims.Width/2)
	c.srv.stepOnSculkSensor(c.theWorld(), x, y, z, pos.X, pos.Y, pos.Z, c.player.UUID, uuid.Nil)
}

// stepOnSculkSensor posts a body's step to the sensor it stands on.
func (s *Server) stepOnSculkSensor(w *world.World, x, y, z int32, bx, by, bz float64, player, mob uuid.UUID) {
	const event = registry.GameEventStep
	s.physicsFor(w).StepOnSensor(x, y, z, world.Vibration{
		X: bx, Y: by, Z: bz, Event: event, Frequency: world.VibrationFrequency(event), Player: player, Mob: mob,
	})
}

// shriekerCanRespond is whether a shrieker may warn and summon.
// vanilla: SculkShriekerBlockEntity.canRespond
func (s *Server) shriekerCanRespond(w *world.World, canSummon bool) bool {
	return canSummon && w.Difficulty() != rules.DifficultyPeaceful && w.BoolRule(world.RuleSpawnWardens)
}

// tryShriek has the shrieker shriek for the player, first raising nearby
// players' warnings if it can respond.
// vanilla: SculkShriekerBlockEntity.tryShriek, WardenSpawnTracker.tryWarn
func (s *Server) tryShriek(w *world.World, x, y, z int32, c *client) {
	if c == nil {
		return
	}
	phys := s.physicsFor(w)
	shrieking, canSummon, ok := phys.ShriekerAt(x, y, z)
	if !ok || shrieking {
		return
	}
	warning := 0
	if s.shriekerCanRespond(w, canSummon) {
		if s.nearbyWarden(w, x, y, z) {
			return
		}
		level, warned := s.warnPlayersAround(w, x, y, z, c)
		if !warned {
			return
		}
		warning = level
	}
	changes, effects := phys.StartShriek(x, y, z, warning)
	s.gameEvent(w, rules.GameEventShriek, float64(x)+0.5, float64(y)+0.5, float64(z)+0.5,
		rules.GameEventContext{Player: c.player.UUID})
	s.tellBlockChanges(w, changes)
	for _, e := range effects {
		s.tellEffect(w, e)
	}
}

// warnPlayersAround is WardenSpawnTracker.tryWarn over living
// non-spectators within 16 blocks of the shrieker, plus the trigger.
func (s *Server) warnPlayersAround(w *world.World, x, y, z int32, trigger *client) (int, bool) {
	cx, cy, cz := float64(x)+0.5, float64(y)+0.5, float64(z)+0.5
	var near []*client
	s.mu.RLock()
	for _, c := range s.clients {
		if c.theWorld() != w || c == trigger {
			continue
		}
		p := c.player.Position()
		dx, dy, dz := p.X-cx, p.Y-cy, p.Z-cz
		if c.player.GameMode() != protocol.GameModeSpectator && !c.player.Vitals().Dead &&
			dx*dx+dy*dy+dz*dz < wardenSearchRadius*wardenSearchRadius {
			near = append(near, c)
		}
	}
	s.mu.RUnlock()
	near = append(near, trigger)
	trackers := make([]*rules.WardenSpawnTracker, len(near))
	s.wardenMu.Lock()
	defer s.wardenMu.Unlock()
	for i, c := range near {
		trackers[i] = &c.wardenTracker
	}
	return rules.WardenWarn(trackers)
}

// shriekRespond ends a shriek: the warning sound near the shrieker and
// darkness on nearby survival players.
// vanilla: SculkShriekerBlockEntity.tryRespond, playWardenReplySound, Warden.applyDarknessAround
func (s *Server) shriekRespond(w *world.World, r world.ShriekResponse) {
	if !s.shriekerCanRespond(w, r.CanSummon) || r.Warning <= 0 {
		return
	}
	summoned := r.Warning >= 4 && s.summonShriekerWarden(w, r.X, r.Y, r.Z)
	if sound, ok := shriekWarningSounds[r.Warning]; ok && !summoned {
		spread := func() float64 { return float64(rand.Intn(2*shriekWarningRadius+1) - shriekWarningRadius) }
		s.broadcastSoundID(w, sound, protocol.SoundCategoryHostile,
			float64(r.X)+spread(), float64(r.Y)+spread(), float64(r.Z)+spread(), 5, 1)
	}
	s.applyWardenDarkness(wardenDarkness{w: w, x: float64(r.X) + 0.5, y: float64(r.Y) + 0.5, z: float64(r.Z) + 0.5, radius: shriekDarknessRadius})
}

// shriekerRemoved answers at once for a shrieking shrieker whose block entity
// was removed; old is the removed cell's state.
// vanilla: SculkShriekerBlockEntity.preRemoveSideEffects
func (s *Server) shriekerRemoved(w *world.World, x, y, z int32, old uint16, be *world.BlockEntity) {
	if be == nil || be.Kind != world.KindSculkShrieker || registry.BlockOf(old) != registry.BlockSculkShrieker ||
		!registry.StateBool(old, registry.PropShrieking) {
		return
	}
	var warning int
	be.Use(func(b *world.BlockEntity) { warning = b.WarningLevel })
	s.shriekRespond(w, world.ShriekResponse{X: x, Y: y, Z: z, Warning: warning, CanSummon: registry.StateBool(old, registry.PropCanSummon)})
}

type wardenDarkness struct {
	w               *world.World
	x, y, z, radius float64
}

// vanilla: MobEffectUtil.addEffectToPlayersAround, Warden.applyDarknessAround
func (s *Server) applyWardenDarkness(at wardenDarkness) {
	darkness := effect.New(registry.MobEffectDarkness, 260, 0, false, false, false)
	cx, cy, cz := at.x, at.y, at.z
	s.mu.RLock()
	var targets []*client
	for _, c := range s.clients {
		p := c.player.Position()
		dx, dy, dz := p.X-cx, p.Y-cy, p.Z-cz
		if c.theWorld() != at.w || dx*dx+dy*dy+dz*dz >= at.radius*at.radius {
			continue
		}
		if m := c.player.GameMode(); m != protocol.GameModeSurvival && m != protocol.GameModeAdventure {
			continue
		}
		if have, ok := c.player.Effect(darkness.ID); ok && have.Amplifier >= darkness.Amplifier && (have.IsInfinite() || have.Duration >= 200) {
			continue
		}
		targets = append(targets, c)
	}
	s.mu.RUnlock()
	for _, c := range targets {
		c.AddEffect(darkness)
	}
}

// vanilla: WardenSpawnTracker.hasNearbyWarden
func (s *Server) nearbyWarden(w *world.World, x, y, z int32) bool {
	s.mobsMu.Lock()
	defer s.mobsMu.Unlock()
	h := s.herdOf(w)
	for _, m := range h.herd {
		if m.WardenSuppressesShrieker(x, y, z) {
			return true
		}
	}
	for _, corpse := range h.dying {
		if corpse.ent.mob.WardenSuppressesShrieker(x, y, z) {
			return true
		}
	}
	return false
}

// vanilla: SculkShriekerBlockEntity.trySummonWarden
func (s *Server) summonShriekerWarden(w *world.World, x, y, z int32) bool {
	dim := s.dimensionOfWorld(w)
	s.mu.RLock()
	var clients []*client
	for _, c := range s.clients {
		clients = append(clients, c)
	}
	s.mu.RUnlock()
	bodies := s.spawnBodies(dim, spawnPlayersOf(clients, w))
	sx, sy, sz, ok := entity.FindWardenSpawnSpot(w, x, y, z, s.levelRng.Intn, bodies)
	if !ok {
		return false
	}
	s.spawnMobArriving(dim, entity.TypeWarden, float64(sx)+0.5, float64(sy), float64(sz)+0.5,
		0, nil, false, "", true, func(ent *mobEntity) { ent.mob.WardenEmerging() })
	s.broadcastSoundID(w, registry.SoundEntityWardenAgitated, protocol.SoundCategoryHostile,
		float64(sx)+0.5, float64(sy), float64(sz)+0.5, 5, 1)
	return true
}
