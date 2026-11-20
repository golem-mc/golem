package server

import (
	"math"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
	"github.com/google/uuid"
)

// The creaking heart's block entity ticker and its link to the creaking: the
// lookups, locks and packets around rules.CreakingProtectorKept and the
// world's spawn and resin searches. The trail the heart draws toward a hurt
// creaking reaches players within 32 blocks, not vanilla's 512.
// vanilla: CreakingHeartBlockEntity

// creakingHurtCall is a heart-bound creaking's word to its heart that a
// player struck it, told after the mob lock.
type creakingHurtCall struct {
	w       *world.World
	id      uuid.UUID
	x, y, z int32
}

// creakingBody is the creaking a heart holds, read under the mob lock.
type creakingBody struct {
	ent        *mobEntity
	feet       [3]float64
	boxMin     [3]float64
	boxMax     [3]float64
	persistent bool
}

func (b creakingBody) centre() [3]float64 {
	return [3]float64{(b.boxMin[0] + b.boxMax[0]) / 2, (b.boxMin[1] + b.boxMax[1]) / 2, (b.boxMin[2] + b.boxMax[2]) / 2}
}

// distanceFrom is the distance from the creaking's feet to the bottom centre
// of the heart's block.
// vanilla: CreakingHeartBlockEntity.distanceToCreaking
func (b creakingBody) distanceFrom(be *world.BlockEntity) float64 {
	return math.Sqrt(entity.Dist2(b.feet[0], b.feet[1], b.feet[2], float64(be.X)+0.5, float64(be.Y), float64(be.Z)+0.5))
}

// heartCentre is the centre of the heart's block.
func heartCentre(be *world.BlockEntity) [3]float64 {
	return [3]float64{float64(be.X) + 0.5, float64(be.Y) + 0.5, float64(be.Z) + 0.5}
}

// creakingHeartAt is the heart's block entity at the cell, nil for none.
func creakingHeartAt(w *world.World, x, y, z int32) *world.BlockEntity {
	if !w.ChunkLoadedAt(x, z) {
		return nil
	}
	if be := w.BlockEntity(x, y, z); be != nil && be.Kind == world.KindCreakingHeart {
		return be
	}
	return nil
}

// creakingOrphaned reports whether a bound creaking's home is loaded and holds
// no heart that lists it. A home in an unloaded chunk is not lost.
// vanilla: Creaking.tick, CreakingHeartBlockEntity.isProtector
func creakingOrphaned(w *world.World, ent *mobEntity) bool {
	x, y, z, bound := ent.mob.CreakingHome()
	if !bound || !w.ChunkLoadedAt(x, z) {
		return false
	}
	be := creakingHeartAt(w, x, y, z)
	if be == nil {
		return true
	}
	listed := false
	be.Use(func(b *world.BlockEntity) { listed = b.Creaking == ent.uuid })
	return !listed
}

// creakingBodyOf is the creaking the heart is linked to; false once it is
// nowhere in the world, and the link is dropped when it has been gone for the
// grace period.
// vanilla: CreakingHeartBlockEntity.getCreakingProtector
func (s *Server) creakingBodyOf(w *world.World, be *world.BlockEntity) (creakingBody, bool) {
	var id uuid.UUID
	var gone int64
	be.Use(func(b *world.BlockEntity) { id, gone = b.Creaking, b.CreakingExisted })
	if id == uuid.Nil {
		return creakingBody{}, false
	}
	var body creakingBody
	found := false
	s.mobsMu.Lock()
	for _, ent := range s.herdOf(w).book {
		if ent.uuid != id {
			continue
		}
		m := ent.mob
		body = creakingBody{ent: ent, feet: [3]float64{m.X, m.Y, m.Z}, persistent: m.Persistence}
		body.boxMin[0], body.boxMin[1], body.boxMin[2], body.boxMax[0], body.boxMax[1], body.boxMax[2] = m.BlastBox()
		found = true
		break
	}
	s.mobsMu.Unlock()
	switch {
	case found:
		// The count is of ticks since it was last seen.
		be.UseLive(func(b *world.BlockEntity) { b.CreakingExisted = 0 })
	case gone >= rules.CreakingGraceTicks:
		be.UseLive(func(b *world.BlockEntity) { b.Creaking = uuid.Nil })
	}
	return body, found
}

// tickCreakingHeart runs the heart's ticker: each tick the comparator strength
// and the particle trail, and every 20 to 24 ticks the state reread, the check
// that the creaking is still wanted and, with none, the spawn of one.
// vanilla: CreakingHeartBlockEntity.serverTick, updateCreakingState
func (s *Server) tickCreakingHeart(w *world.World, be *world.BlockEntity) {
	state := w.BlockAt(be.X, be.Y, be.Z)
	if registry.BlockOf(state) != registry.BlockCreakingHeart {
		return
	}
	if registry.StateValue(state, registry.PropCreakingHeartState) == "uprooted" {
		// The ticker runs only on logs; an uprooted heart waits for the block tick
		// a neighbour change books, which restores it once its logs return.
		// vanilla: CreakingHeartBlock.getTicker
		return
	}
	body, found := s.creakingBodyOf(w, be)
	s.creakingOutput(w, be, body, found)
	var emitter int32
	var target [3]float64
	be.UseLive(func(b *world.BlockEntity) {
		b.CreakingExisted++
		emitter, target = b.CreakingEmitter, b.CreakingTarget
		if b.CreakingEmitter > 0 {
			b.CreakingEmitter--
		}
	})
	if emitter > 0 {
		s.creakingTrail(w, be, body, found, emitter, target)
	}
	due := false
	be.UseLive(func(b *world.BlockEntity) {
		if b.CreakingTicker >= 0 {
			b.CreakingTicker--
			return
		}
		b.CreakingTicker = int32(s.levelRng.Intn(5) + 20)
		due = true
	})
	if !due {
		return
	}
	linked := false
	be.Use(func(b *world.BlockEntity) { linked = b.Creaking != uuid.Nil })
	next, changed := world.CreakingHeartNextState(w, state, be.X, be.Y, be.Z, linked)
	if changed {
		// The write tells clients and wakes neighbours, comparators included.
		// vanilla: Level.setBlock flag 3
		if w.SetBlock(be.X, be.Y, be.Z, next) {
			s.markChanged(w, be.X, be.Y, be.Z)
			s.blockChanged(w, be.X, be.Y, be.Z, state)
		}
		state = next
		if registry.StateValue(next, registry.PropCreakingHeartState) == "uprooted" {
			return
		}
	}
	if linked {
		s.creakingCheck(w, be, body, found)
		return
	}
	awake := registry.StateValue(state, registry.PropCreakingHeartState) == "awake"
	if rules.CreakingMaySpawn(awake, spawningMonsters(w), w.Difficulty() == rules.DifficultyPeaceful) &&
		s.creakingPlayerNear(w, be) {
		s.creakingSpawnProtector(w, be)
	}
}

// creakingOutput keeps the comparator strength the creaking's distance gives
// and wakes the comparators reading it when it moves.
// vanilla: CreakingHeartBlockEntity.serverTick
func (s *Server) creakingOutput(w *world.World, be *world.BlockEntity, body creakingBody, found bool) {
	out := 0
	if found {
		out = rules.CreakingHeartOutput(body.distanceFrom(be))
	}
	moved := false
	be.UseLive(func(b *world.BlockEntity) {
		moved = b.CreakingOutput != out
		b.CreakingOutput = out
	})
	if moved {
		s.physicsFor(w).EntityOutputChanged(be.X, be.Y, be.Z)
	}
}

// creakingCheck lets the creaking go when it has lost its night, strayed past
// 34 blocks or has a player's eyes inside it for too many checks.
// vanilla: CreakingHeartBlockEntity.serverTick
func (s *Server) creakingCheck(w *world.World, be *world.BlockEntity, body creakingBody, found bool) {
	if !found {
		return
	}
	s.mobsMu.Lock()
	stuck := body.ent.mob.CreakingStuckCheck()
	s.mobsMu.Unlock()
	if !rules.CreakingProtectorKept(w.CreakingActive(), body.persistent, body.distanceFrom(be), stuck) {
		s.creakingRemoveProtector(w, be, false)
	}
}

// creakingPlayerNear reports whether a player who is not a spectator stands
// within 32 blocks of the heart.
// vanilla: Level.getNearestPlayer
func (s *Server) creakingPlayerNear(w *world.World, be *world.BlockEntity) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, c := range s.clients {
		if c.theWorld() != w || c.player.GameMode() == protocol.GameModeSpectator {
			continue
		}
		pos := c.player.Position()
		if entity.Dist2(pos.X, pos.Y, pos.Z, float64(be.X), float64(be.Y), float64(be.Z)) <
			rules.CreakingPlayerRange*rules.CreakingPlayerRange {
			return true
		}
	}
	return false
}

// creakingSpawnProtector stands a creaking up near the heart and links it.
// vanilla: CreakingHeartBlockEntity.spawnProtector
func (s *Server) creakingSpawnProtector(w *world.World, be *world.BlockEntity) {
	x, y, z, ok := world.CreakingSpawnSpot(w, s.levelRng, be.X, be.Y, be.Z)
	if !ok {
		return
	}
	var id uuid.UUID
	var voice registry.Sound
	var volume, pitch, spawnVolume, spawnPitch float32
	// The link is set before the body joins the herd, whose first tick asks the
	// heart for it.
	_, err := s.spawnSummonedMobFacing(s.dimensionOfWorld(w), entity.TypeCreaking, x, y, z, nbt.Compound{}, func(ent *mobEntity) {
		id = ent.uuid
		ent.mob.CreakingBind(be.X, be.Y, be.Z)
		voice, volume, pitch = ent.mob.AmbientVoice()
		spawnVolume, spawnPitch = ent.mob.SoundVolume(), ent.mob.VoicePitch()
		be.UseLive(func(b *world.BlockEntity) { b.Creaking, b.CreakingExisted = id, 0 })
	})
	if err != nil {
		be.UseLive(func(b *world.BlockEntity) { b.Creaking = uuid.Nil })
		return
	}
	s.gameEvent(w, rules.GameEventEntityPlace, x, y, z, rules.GameEventContext{Mob: id})
	// vanilla: SpawnUtil.trySpawnMob plays the ambient sound
	s.broadcastVoiceID(w, entity.TypeCreaking, false, x, y, z, voice, volume, pitch)
	s.broadcastVoiceID(w, entity.TypeCreaking, false, x, y, z, registry.SoundEntityCreakingSpawn, spawnVolume, spawnPitch)
	s.broadcastSoundID(w, registry.SoundBlockCreakingHeartSpawn, protocol.SoundCategoryBlock,
		float64(be.X)+0.5, float64(be.Y)+0.5, float64(be.Z)+0.5, 1, 1)
}

// creakingRemoveProtector lets go of the heart's creaking: killed by a player's
// break it twitches and dies; otherwise it is taken away in a crumble of wood.
// The crumble that ends a twitch is not drawn.
// vanilla: CreakingHeartBlockEntity.removeProtector, Creaking.tearDown
func (s *Server) creakingRemoveProtector(w *world.World, be *world.BlockEntity, killed bool) {
	body, found := s.creakingBodyOf(w, be)
	if !found {
		return
	}
	s.mobsMu.Lock()
	m := body.ent.mob
	if killed {
		m.CreakingKillByHeart()
	} else {
		m.CreakingTearDown()
	}
	height := body.boxMax[1] - body.boxMin[1]
	x, y, z, silent := m.X, m.Y, m.Z, m.Silent
	volume, pitch := m.SoundVolume(), m.VoicePitch()
	s.mobsMu.Unlock()
	c := body.centre()
	if killed {
		s.broadcastVoiceID(w, entity.TypeCreaking, silent, x, y, z, registry.SoundEntityCreakingTwitch, volume, pitch)
	} else {
		s.creakingCrumble(w, c, height)
		s.broadcastVoiceID(w, entity.TypeCreaking, silent, x, y, z, registry.SoundEntityCreakingDeath, volume, pitch)
	}
	be.UseLive(func(b *world.BlockEntity) { b.Creaking = uuid.Nil })
}

// creakingCrumble is the cloud of wood and heart chips a creaking taken away
// leaves, spread by 0.3 of its box.
// vanilla: Creaking.tearDown
func (s *Server) creakingCrumble(w *world.World, c [3]float64, height float64) {
	const id = protocol.ParticleBlockCrumble
	spread := float32(height * 0.3)
	s.sendBlockParticles(w, id, registry.BlockPaleOakWood.DefaultState(), c[0], c[1], c[2], spread, spread, spread, 0, 100)
	if heart, ok := registry.WithProp(registry.BlockCreakingHeart.DefaultState(), registry.PropCreakingHeartState, "awake"); ok {
		s.sendBlockParticles(w, id, heart, c[0], c[1], c[2], spread, spread, spread, 0, 10)
	}
}

// creakingHeartBroken is the heart's part of a player's break: its creaking
// dies, and a natural heart pays its experience.
// vanilla: CreakingHeartBlock.playerWillDestroy
func (s *Server) creakingHeartBroken(w *world.World, x, y, z int32, state uint16, mode int32) {
	be := creakingHeartAt(w, x, y, z)
	if be == nil {
		return
	}
	s.creakingRemoveProtector(w, be, true)
	s.creakingHeartExperience(w, x, y, z, state, mode)
}

// creakingHeartExploded is the heart's part of a blast that destroys it: its
// creaking dies, and a natural heart pays its experience when a player is the
// blast's indirect source.
// vanilla: CreakingHeartBlock.onExplosionHit
func (s *Server) creakingHeartExploded(b blast, x, y, z int32, state uint16) {
	be := creakingHeartAt(b.w, x, y, z)
	if be == nil {
		return
	}
	s.creakingRemoveProtector(b.w, be, true)
	s.mu.RLock()
	c := s.clients[b.causeUUID]
	s.mu.RUnlock()
	if c != nil {
		s.creakingHeartExperience(b.w, x, y, z, state, c.player.GameMode())
	}
}

// creakingHeartExperience pays 20 to 24 experience for a natural heart unless
// the player is in creative or spectator mode.
// vanilla: CreakingHeartBlock.tryAwardExperience
func (s *Server) creakingHeartExperience(w *world.World, x, y, z int32, state uint16, mode int32) {
	if mode != protocol.GameModeCreative && mode != protocol.GameModeSpectator &&
		registry.StateValue(state, registry.PropNatural) == "true" && w.BoolRule(world.RuleBlockDrops) {
		s.spawnOrbs(w, float64(x)+0.5, float64(y)+0.5, float64(z)+0.5, s.levelRng.Intn(5)+20)
	}
}

// creakingHurtHeart is a player's blow on a creaking its heart holds: the
// trail of particles, and by night the resin it grows on the logs. A trail
// already running is not restarted.
// vanilla: Creaking.hurtServer, CreakingHeartBlockEntity.creakingHurt
func (s *Server) creakingHurtHeart(call creakingHurtCall) {
	be := creakingHeartAt(call.w, call.x, call.y, call.z)
	if be == nil {
		return
	}
	body, found := s.creakingBodyOf(call.w, be)
	if !found || body.ent.uuid != call.id {
		return
	}
	running := false
	be.UseLive(func(b *world.BlockEntity) {
		if running = b.CreakingEmitter > 0; running {
			return
		}
		b.CreakingEmitter, b.CreakingTarget = rules.CreakingTrailTicks, body.centre()
	})
	if running {
		return
	}
	s.creakingParticles(call.w, be, body, rules.CreakingHurtParticles, false)
	if registry.StateValue(call.w.BlockAt(be.X, be.Y, be.Z), registry.PropCreakingHeartState) != "awake" {
		return
	}
	for range s.levelRng.Intn(2) + 2 {
		rx, ry, rz, state, ok := world.CreakingResinSpread(call.w, s.levelRng, be.X, be.Y, be.Z)
		if !ok {
			continue
		}
		s.setBlockAndTell(call.w, rx, ry, rz, state)
		s.broadcastSoundID(call.w, registry.SoundBlockResinPlace, protocol.SoundCategoryBlock,
			float64(rx)+0.5, float64(ry)+0.5, float64(rz)+0.5, 1, 1)
	}
}

// creakingTrail is one tick of a running trail: a stream each way while more
// than half of it is left, and every tenth tick the heart's hurt sound
// travelling toward the heart from where the creaking last was.
// vanilla: CreakingHeartBlockEntity.serverTick
func (s *Server) creakingTrail(w *world.World, be *world.BlockEntity, body creakingBody, found bool, emitter int32, target [3]float64) {
	if emitter > rules.CreakingTrailStream {
		s.creakingParticles(w, be, body, 1, true)
		s.creakingParticles(w, be, body, 1, false)
	}
	if emitter%10 != 0 {
		return
	}
	if found {
		target = body.centre()
		be.UseLive(func(b *world.BlockEntity) { b.CreakingTarget = target })
	}
	x, y, z, volume := rules.CreakingHurtSound(emitter, target, heartCentre(be))
	s.broadcastSoundID(w, registry.SoundBlockCreakingHeartHurt, protocol.SoundCategoryBlock,
		float64(x)+0.5, float64(y)+0.5, float64(z)+0.5, volume, 1)
}

// creakingParticles sends count trail particles between the creaking's box and
// the heart's block: orange toward the creaking, gray toward the heart.
// vanilla: CreakingHeartBlockEntity.emitParticles
func (s *Server) creakingParticles(w *world.World, be *world.BlockEntity, body creakingBody, count int, towardsCreaking bool) {
	if body.ent == nil {
		return
	}
	color := rules.CreakingTrailGray
	if towardsCreaking {
		color = rules.CreakingTrailOrange
	}
	heart := [3]float64{float64(be.X), float64(be.Y), float64(be.Z)}
	for range count {
		p := rules.CreakingTrail(s.levelRng, body.boxMin, body.boxMax, heart, towardsCreaking)
		s.sendParticlePayload(w, trailParticlePayload(p.X, p.Y, p.Z, p.ToX, p.ToY, p.ToZ, color, p.Ticks, true, true), p.X, p.Y, p.Z)
	}
}
