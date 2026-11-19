package server

import (
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/effect"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
	"github.com/google/uuid"
)

func (s *Server) dragonEffect(shot ghastShot) {
	switch shot.kind {
	case entity.MobDragonShot:
		if !shot.silent {
			s.broadcastLevelEvent(shot.w, 1017, shot.bx, shot.by, shot.bz, 0)
		}
		s.spawnFireball(entity.ThrownDragonFireball, shot.w, shot.x, shot.y, shot.z, shot.dx, shot.dy, shot.dz, shot.shooter, shot.uuid, 0, shot.seed)
	case entity.MobDragonFlame:
		s.spawnDragonCloud(shot.w, shot.x, shot.y, shot.z, shot.uuid, true)
	case entity.MobDragonFlameEnded:
		s.arrowsMu.Lock()
		var clouds []*cloudEntity
		for _, it := range s.clouds {
			if it.w == shot.w && it.ownerUUID == shot.uuid && it.flame {
				clouds = append(clouds, it)
			}
		}
		s.arrowsMu.Unlock()
		for _, it := range clouds {
			s.killCloud(it)
		}
	}
}

// vanilla: DragonFireball.onHit, DragonSittingFlamingPhase.doServerTick
func (s *Server) spawnDragonCloud(w *world.World, x, y, z float64, owner uuid.UUID, flame bool) {
	s.mobsMu.Lock()
	ownerMob := s.dimensionOfWorld(w).mobs.byUUID[owner]
	ownerDragon := ownerMob != nil && ownerMob.Type == entity.TypeEnderDragon
	s.mobsMu.Unlock()
	amp, duration, radius, growth := 0, 200, float32(5), float32(0)
	if !flame {
		amp, duration, radius, growth = 1, 600, 3, 4.0/600
	}
	in := effect.New(registry.MobEffectInstantDamage, 1, int32(amp), false, true, true)
	in = rules.LingeringInstance(in, 0.25)
	cloud := entity.NewCloud(s.nextEntity(), x, y, z, []effect.Instance{in})
	cloud.WaitTime, cloud.Duration, cloud.RadiusOnUse = 20, duration, 0
	cloud.Restore(0, radius, growth)
	s.registerCloud(&cloudEntity{cloud: cloud, w: w, uuid: uuid.New(), ownerUUID: owner, ownerDragon: ownerDragon, flame: flame, fresh: true,
		durationScale: 0.25, contents: protocol.PotionContents{Potion: protocol.NoPotion, CustomEffects: []protocol.PotionEffectSpec{{ID: in.ID, Duration: 1, Amplifier: int32(amp), ShowParticles: true, ShowIcon: true}}},
		customParticle: nbt.Compound{"type": nbt.String("minecraft:dragon_breath"), "power": nbt.Float(1)}})
}

func (it *cloudEntity) dragonParticle() bool {
	particle, ok := nbt.CompoundView(it.customParticle)
	return ok && particle["type"] == nbt.String("minecraft:dragon_breath")
}
