package server

import (
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/registry"
)

// vanilla: SonicBoom.tick
func (s *Server) sonicBoom(strike mobStrike, victim *client) {
	ray := strike.sonic
	const particle = protocol.ParticleSonicBoom
	for i := 1; i < ray.Steps; i++ {
		s.sendParticles(strike.w, particle, strike.x+ray.DX*float64(i), strike.y+1.6+ray.DY*float64(i), strike.z+ray.DZ*float64(i), 0, 0, 0, 0, 1)
	}
	if victim != nil {
		// sonic_boom scales for players and bypasses shields,
		// armour and Protection; Resistance and the hurt window still apply.
		landed, softened := victim.hurt(rules.ScaledWithDifficulty(strike.damage, s.world.Difficulty()), registry.DamageTypeSonicBoom, &damageCause{
			entityID: strike.mob.ID, x: strike.x, y: strike.y, z: strike.z, killer: strike.killer, weapon: strike.mob.MainHand,
		})
		if landed {
			if !softened {
				victim.knockbackFromPoint(strike.w, strike.x, strike.y, strike.z, syncVelocityAfterTravel)
			}
			var resistance float64
			victim.player.Attributes(func(a *player.Attributes) {
				resistance = a.Value(registry.AttributeKnockbackResistance)
			})
			x, y, z := ray.Push(resistance)
			victim.player.AddVelocity(x, y, z)
			victim.syncVelocity(strike.w, victim.player.Velocity(), syncVelocityAfterTravel)
		}
		return
	}
	s.mobsMu.Lock()
	var target *entity.Mob
	for _, m := range s.herdOf(strike.w).herd {
		if m.ID == strike.victimID {
			target = m
			break
		}
	}
	s.mobsMu.Unlock()
	if target != nil {
		s.hurtMobByThen(strike.w, target, strike.mob, strike.damage, registry.DamageTypeSonicBoom, func() {
			x, y, z := ray.Push(target.KnockbackResistance())
			target.Push(x, y, z)
		})
	}
}
