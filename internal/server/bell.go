package server

import (
	"slices"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/effect"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
	"github.com/google/uuid"
)

const (
	bellShakeTicks     = 50
	bellGlowTicks      = 60
	bellSearchEvery    = 60
	bellResonateTicks  = 40
	bellTicksBeforeRes = 5
	bellSearchRadius   = 48
	bellHearRadius     = 32
)

// bellRung starts the swing of a bell that took a ring block event and
// refreshes its raider list. Runs where the block event is applied, with no
// lock held.
// vanilla: BellBlockEntity.triggerEvent, updateEntities
func (s *Server) bellRung(w *world.World, x, y, z int32) {
	be := w.EnsureBlockEntity(x, y, z, world.KindBell)
	if be == nil {
		return
	}
	now, _ := s.world.Time()
	var search bool
	be.UseLive(func(b *world.BlockEntity) {
		b.Bell.ResonationTicks, b.Bell.Ticks, b.Bell.Shaking = 0, 0, true
		search = now > b.Bell.SearchedAt+bellSearchEvery || b.Bell.Raiders == nil
		if search {
			b.Bell.SearchedAt = now
		}
	})
	if !search {
		return
	}
	raiders := []uuid.UUID{}
	s.mobsMu.Lock()
	for m, ent := range s.herdOf(w).book {
		if !entity.IsRaiderType(m.Type) {
			continue
		}
		minX, minY, minZ, maxX, maxY, maxZ := m.BlastBox()
		if maxX >= float64(x-bellSearchRadius) && minX <= float64(x+1+bellSearchRadius) &&
			maxY >= float64(y-bellSearchRadius) && minY <= float64(y+1+bellSearchRadius) &&
			maxZ >= float64(z-bellSearchRadius) && minZ <= float64(z+1+bellSearchRadius) {
			raiders = append(raiders, ent.uuid)
		}
	}
	s.mobsMu.Unlock()
	be.UseLive(func(b *world.BlockEntity) { b.Bell.Raiders = raiders })
}

// tickBell advances a bell's swing and resonance: from tick 5 of a swing a
// live raider within 32 blocks makes it resonate, and 40 ticks later every
// raider within 48 blocks glows.
// vanilla: BellBlockEntity.serverTick
func (s *Server) tickBell(dim *dimension, be *world.BlockEntity) {
	var raiders []uuid.UUID
	var check, ending bool
	be.UseLive(func(b *world.BlockEntity) {
		bell := &b.Bell
		if bell.Shaking {
			bell.Ticks++
		}
		if bell.Ticks >= bellShakeTicks {
			bell.Shaking, bell.Ticks = false, 0
		}
		check = bell.Ticks >= bellTicksBeforeRes && bell.ResonationTicks == 0
		ending = bell.Resonating && bell.ResonationTicks >= bellResonateTicks
		if check || ending {
			raiders = slices.Clone(bell.Raiders)
		}
	})
	if !check && !ending {
		be.UseLive(func(b *world.BlockEntity) {
			if b.Bell.Resonating {
				b.Bell.ResonationTicks++
			}
		})
		return
	}
	cx, cy, cz := float64(be.X)+0.5, float64(be.Y)+0.5, float64(be.Z)+0.5
	within := func(m *entity.Mob, r float64) bool {
		dx, dy, dz := m.X-cx, m.Y-cy, m.Z-cz
		return m.Alive() && dx*dx+dy*dy+dz*dz < r*r
	}
	var near bool
	var tells []splashMobTell
	const glowing = registry.MobEffectGlowing
	s.lockTick(&s.mobsMu)
	herd := dim.mobs
	for _, id := range raiders {
		m := herd.byUUID[id]
		if m == nil {
			continue
		}
		if check && within(m, bellHearRadius) {
			near = true
		}
		if ending && within(m, bellSearchRadius) {
			ch, added := m.AddEffect(effect.New(glowing, bellGlowTicks, 0, false, true, true))
			if !added {
				continue
			}
			if tell, ok := mobEffectTell(dim, m, []effect.Change{ch}); ok {
				tells = append(tells, tell)
			}
		}
	}
	s.unlockTick(&s.mobsMu)
	for _, t := range tells {
		s.tellSplashMob(dim.w, t)
	}
	var resonated bool
	be.UseLive(func(b *world.BlockEntity) {
		bell := &b.Bell
		if check && near {
			bell.Resonating, resonated = true, true
		}
		if bell.Resonating {
			if bell.ResonationTicks < bellResonateTicks {
				bell.ResonationTicks++
			} else {
				bell.Resonating = false
			}
		}
	})
	if resonated {
		s.broadcastNear(dim.w, cx, cy, cz, soundRange(1.0), protocol.IDPlaySoundEffect,
			soundEffectIDAt(registry.SoundBlockBellResonate, protocol.SoundCategoryBlock, cx, cy, cz, 1, 1))
	}
}
