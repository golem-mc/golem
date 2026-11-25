package server

import (
	"github.com/golem-mc/golem/internal/server/effect"
	"github.com/golem-mc/golem/registry"
	"slices"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
	"github.com/google/uuid"
)

// tickConduit runs a conduit's turn: on its refresh stride it rescans the
// frame, powers swimmers in reach and hunts with a full cage; every tick an
// active conduit plays its ambient. The refresh reads the entity live, so a
// break since the list was read leaves it running on nothing.
// vanilla: ConduitBlockEntity.serverTick
func (s *Server) tickConduit(w *world.World, be *world.BlockEntity, age int64) {
	x, y, z := be.X, be.Y, be.Z
	if age%rules.ConduitRefreshTicks == 0 {
		frame := rules.ConduitFrame(w, x, y, z)
		active := frame >= rules.ConduitMinFrame
		var was, live bool
		be.UseLive(func(b *world.BlockEntity) {
			live, was = true, b.ConduitActive
			b.ConduitActive = active
		})
		if !live {
			return
		}
		if active != was {
			sound := rules.ConduitToggleSound(active)
			cx, cy, cz := float64(x)+0.5, float64(y)+0.5, float64(z)+0.5
			s.broadcastNear(w, cx, cy, cz, soundRange(1.0), protocol.IDPlaySoundEffect,
				soundEffectIDAt(sound, protocol.SoundCategoryBlock, cx, cy, cz, 1, 1))
		}
		if active {
			s.conduitPowers(w, x, y, z, frame)
			s.conduitHunts(w, be, frame >= rules.ConduitHuntFrame)
		}
	}
	s.conduitAmbient(w, be, age)
}

// conduitAmbient plays an active conduit's sounds: the pulse every 80
// ticks, and a short ambient 60 to 99 ticks after the last.
// vanilla: ConduitBlockEntity.serverTick
func (s *Server) conduitAmbient(w *world.World, be *world.BlockEntity, age int64) {
	var active, short bool
	be.UseLive(func(b *world.BlockEntity) {
		if !b.ConduitActive {
			return
		}
		active = true
		b.ConduitNextAmbient, short = rules.ConduitShortAmbient(age, b.ConduitNextAmbient, s.levelRng)
	})
	if !active {
		return
	}
	cx, cy, cz := float64(be.X)+0.5, float64(be.Y)+0.5, float64(be.Z)+0.5
	if rules.ConduitAmbientPulse(age) {
		s.broadcastNear(w, cx, cy, cz, soundRange(1.0), protocol.IDPlaySoundEffect,
			soundEffectIDAt(registry.SoundBlockConduitAmbient, protocol.SoundCategoryBlock, cx, cy, cz, 1, 1))
	}
	if short {
		s.broadcastNear(w, cx, cy, cz, soundRange(1.0), protocol.IDPlaySoundEffect,
			soundEffectIDAt(registry.SoundBlockConduitAmbientShort, protocol.SoundCategoryBlock, cx, cy, cz, 1, 1))
	}
}

// conduitPowers gives Conduit Power to swimmers an active frame reaches.
// vanilla: ConduitBlockEntity.applyEffects
func (s *Server) conduitPowers(w *world.World, x, y, z int32, frame int) {
	reach := rules.ConduitEffectRange(frame)
	s.mu.RLock()
	clients := s.clientsLocked()
	s.mu.RUnlock()
	for _, c := range clients {
		if c.theWorld() != w {
			continue
		}
		// vanilla: EntitySelector.NO_SPECTATORS
		if c.player.GameMode() == protocol.GameModeSpectator {
			continue
		}
		pos := c.player.Position()
		if rules.ConduitReaches(x, y, z, pos.X, pos.Y, pos.Z, reach) && c.inWaterOrRain(pos) {
			c.effectCause = effect.CauseConduit
			c.AddEffect(rules.ConduitPower())
		}
	}
}

// conduitHunts runs the beam: a hunting conduit keeps its current target
// (the live mob it names, within hunt range) or picks a new one from the
// cube's mobs, telling clients whenever the target changes.
// vanilla: ConduitBlockEntity.updateAndAttackTarget, selectNewTarget
func (s *Server) conduitHunts(w *world.World, be *world.BlockEntity, hunting bool) {
	x, y, z := be.X, be.Y, be.Z
	var had uuid.UUID
	be.Use(func(b *world.BlockEntity) { had = b.Target })

	next := uuid.Nil
	var strike *conduitStrike
	s.mobsMu.Lock()
	herd := s.herdOf(w)
	if hunting {
		if had == uuid.Nil {
			if bound := conduitCandidates(w, x, y, z, herd); len(bound) > 0 {
				ent := bound[s.levelRng.Intn(len(bound))]
				next, strike = ent.uuid, s.conduitStrikeOf(ent)
			}
		} else if ent := conduitTargetEntity(herd, had, x, y, z); ent != nil {
			next, strike = had, s.conduitStrikeOf(ent)
		}
	}
	s.mobsMu.Unlock()

	if strike != nil {
		strike.tell(s, w)
	}
	if next != had {
		written := false
		be.UseLive(func(b *world.BlockEntity) { b.Target = next; written = true })
		if written {
			s.tellConduitTag(w, be)
		}
	}
}

// conduitTargetEntity returns the current target if it can be kept (the
// live mob its UUID names, within range), nil if it died, despawned or left
// range. vanilla: ConduitBlockEntity.updateDestroyTarget
func conduitTargetEntity(herd *mobHerd, target uuid.UUID, x, y, z int32) *mobEntity {
	if target == uuid.Nil {
		return nil
	}
	m := herd.byUUID[target]
	ent := herd.book[m]
	if ent == nil || !m.Alive() || !rules.ConduitReaches(x, y, z, m.X, m.Y, m.Z, rules.ConduitHuntRange) {
		return nil
	}
	return ent
}

// conduitCandidates lists the Enemy mobs whose box meets the hunt cube and
// that are in water or rain. vanilla: ConduitBlockEntity.selectNewTarget
func conduitCandidates(w *world.World, x, y, z int32, herd *mobHerd) []*mobEntity {
	var bound []*mobEntity
	for m, ent := range herd.book {
		if !m.Alive() || !entity.Enemy(m.Type) {
			continue
		}
		minX, minY, minZ, maxX, maxY, maxZ := m.BlastBox()
		if !rules.ConduitHunts(x, y, z, minX, minY, minZ, maxX, maxY, maxZ) {
			continue
		}
		if !m.InWater() && !rules.RainsOnBody(w, m.X, m.Y, m.Z, maxY) {
			continue
		}
		bound = append(bound, ent)
	}
	return bound
}

// conduitStrike is one beam hit on one body, gathered under the herd's lock
// and told outside it.
type conduitStrike struct {
	id       int32
	x, y, z  float64
	kind     entity.MobType
	silent   bool
	sound    protocol.SoundHolder
	volume   float32
	pitch    float32
	landed   bool
	softened bool
	health   []byte
}

// conduitStrikeOf deals the beam's 4 magic damage to the hunted mob, under
// the usual invulnerability window, and gathers what to tell. The new
// health goes into every watcher's record so the mob pass does not resend
// it. vanilla: ConduitBlockEntity.updateAndAttackTarget
func (s *Server) conduitStrikeOf(ent *mobEntity) *conduitStrike {
	m := ent.mob
	landed, softened := m.HurtByWorld(rules.ConduitAttackDamage, rules.DamageTypeMagic)
	strike := &conduitStrike{
		id: m.ID, x: m.X, y: m.Y, z: m.Z,
		kind: m.Type, silent: m.Silent,
		sound: mobSound(m, true), volume: m.SoundVolume(), pitch: m.VoicePitch(),
		landed: landed, softened: softened,
	}
	if landed {
		strike.health = ent.syncHealth()
	}
	return strike
}

// syncHealth records the mob's health for every watcher and returns its
// metadata payload. The herd's lock is held.
func (ent *mobEntity) syncHealth() []byte {
	ent.updateShownMetadata(func(state *mobMetadataState) { state.health = float32(ent.mob.Health) })
	return mobHealthPayload(ent)
}

// tell sends a strike: the beam's sound at the body, then the health and,
// for a whole blow, the damage event and hurt sound. It reads fields copied
// under the herd's lock, so it cannot use mobHurtTold, and unlike that it
// sends the health too. A softened blow is told by health alone.
func (strike *conduitStrike) tell(s *Server, w *world.World) {
	s.broadcastNear(w, strike.x, strike.y, strike.z, soundRange(1.0), protocol.IDPlaySoundEffect,
		soundEffectIDAt(registry.SoundBlockConduitAttackTarget, protocol.SoundCategoryBlock,
			strike.x, strike.y, strike.z, 1, 1))
	if !strike.landed || strike.softened {
		if strike.health != nil {
			s.broadcastWorld(w, protocol.IDPlayEntityMetadata, strike.health)
		}
		return
	}
	s.broadcastWorld(w, protocol.IDPlayDamageEvent, damageEventPayload(strike.id, rules.DamageTypeMagic, 0, 0))
	if strike.health != nil {
		s.broadcastWorld(w, protocol.IDPlayEntityMetadata, strike.health)
	}
	s.broadcastVoice(w, strike.kind, strike.silent, strike.x, strike.y, strike.z,
		strike.sound, strike.volume, strike.pitch)
}

// tellConduitTag sends the conduit's update tag (the beam's target), which
// draws the beam, to the whole world whenever the target changes.
// vanilla: ConduitBlockEntity.getUpdateTag
func (s *Server) tellConduitTag(w *world.World, be *world.BlockEntity) {
	var data nbt.Compound
	be.Use(func(b *world.BlockEntity) { data = b.UpdateTag() })
	s.broadcastBlockChunkExcept(w, nil, be.X, be.Z, protocol.IDPlayBlockEntityData, blockEntityData(be.X, be.Y, be.Z, be.TypeID(), data))
}

// inWaterOrRain reports whether the body is in water (anywhere in its box)
// or rained on (at the feet cell or the box top).
// vanilla: Entity.isInWaterOrRain
func (c *client) inWaterOrRain(pos player.Position) bool {
	dims := c.dims(c.player.Appearance().Pose)
	cells, _, _ := c.overlapCells(pos, dims.Width/2, dims.Height)
	if slices.ContainsFunc(cells, rules.IsWater) {
		return true
	}
	return rules.RainsOnBody(c.theWorld(), pos.X, pos.Y, pos.Z, pos.Y+dims.Height)
}
