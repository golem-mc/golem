package server

import (
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/effect"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/world"
)

type mannequinTargetSnapshot struct {
	byWorld map[*world.World][]entity.ArrowTarget
	byID    map[int32]*mannequinEntity
}

// mannequinTargets reads living mannequins before the projectile locks are taken.
func (s *Server) mannequinTargets() mannequinTargetSnapshot {
	s.lockTick(&s.mannequinsMu)
	defer s.unlockTick(&s.mannequinsMu)
	if len(s.mannequins) == 0 {
		return mannequinTargetSnapshot{}
	}
	snapshot := mannequinTargetSnapshot{
		byWorld: make(map[*world.World][]entity.ArrowTarget),
		byID:    make(map[int32]*mannequinEntity, len(s.mannequins)),
	}
	for _, ent := range s.mannequins {
		m := ent.mannequin
		if m.Dead || m.Dying() {
			continue
		}
		minX, minY, minZ, maxX, maxY, maxZ := m.BlastBox()
		snapshot.byID[m.ID] = ent
		snapshot.byWorld[ent.w] = append(snapshot.byWorld[ent.w], entity.ArrowTarget{
			EntityID: m.ID, Type: protocol.EntityTypeMannequin,
			MinX: minX, MinY: minY, MinZ: minZ, MaxX: maxX, MaxY: maxY, MaxZ: maxZ,
		})
	}
	return snapshot
}

// mannequinEffectPackets synchronizes effect attributes and metadata with the lock held.
func mannequinEffectPackets(ent *mannequinEntity) []outbound {
	m := ent.mannequin
	rules.UpdateEffectAttributes(m.Attributes, m.AllEffects())
	m.Health = min(m.Health, m.HealthCap())
	var packets []outbound
	// The next tracking pass sends the metadata delta after the movement.
	if attrs := m.Attributes.TakeDirty(); len(attrs) > 0 {
		packets = append(packets, outbound{id: protocol.IDPlayUpdateAttributes, data: attributesPayload(m.ID, attrs)})
	}
	return packets
}

type mannequinEffectTell struct {
	w       *world.World
	ent     *mannequinEntity
	effects []effect.Instance
	scale   float64
	src     instantSource
}

// landMannequinEffects applies a splash or cloud after the projectile locks open.
func (s *Server) landMannequinEffects(tell mannequinEffectTell) {
	s.mannequinsMu.Lock()
	ent := tell.ent
	if ent == nil || ent.w != tell.w || ent.mannequin.Dead || ent.mannequin.Dying() {
		s.mannequinsMu.Unlock()
		return
	}
	m := ent.mannequin
	var hurts []mannequinHurt
	landLivingEffects(protocol.EntityTypeMannequin, tell.effects, tell.scale, func(in effect.Instance) (effect.Change, bool) {
		ch, ok := m.AddEffect(in)
		if ok {
			rules.UpdateEffectAttributes(m.Attributes, m.AllEffects())
		}
		return ch, ok
	}, m.Heal, func(_ effect.Instance, amount float64) {
		cause := int32(0)
		if tell.src.ownerID != 0 {
			cause = tell.src.ownerID + 1
		}
		shove := &mannequinKnockback{x: tell.src.bodyX, z: tell.src.bodyZ}
		if hurt, landed := s.hurtMannequinLocked(ent, amount, rules.DamageTypeIndirectMagic, cause, tell.src.bodyID+1, nil, tell.src.ownerCreative, shove); landed {
			hurts = append(hurts, hurt)
		}
	})
	packets := mannequinEffectPackets(ent)
	watchers := trackedWatchers(ent.tracked)
	s.mannequinsMu.Unlock()
	for _, hurt := range hurts {
		s.tellMannequinHurt(hurt)
	}
	s.tellWatchers(tell.w, watchers, packets)
}

func (s *Server) douseMannequin(w *world.World, ent *mannequinEntity) {
	s.mannequinsMu.Lock()
	if ent == nil || ent.w != w || ent.mannequin.Dead || ent.mannequin.Dying() || ent.mannequin.FireTicks <= 0 {
		s.mannequinsMu.Unlock()
		return
	}
	m := ent.mannequin
	m.FireTicks = 0
	x, y, z := m.X, m.Y, m.Z
	packets, watchers := mannequinEffectPackets(ent), trackedWatchers(ent.tracked)
	s.mannequinsMu.Unlock()
	s.broadcastSoundPacketExcept(w, nil, s.extinguishSound(protocol.SoundCategoryNeutral, x, y, z))
	s.tellWatchers(w, watchers, packets)
}
