package server

import (
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// bodyLanding is a loose body (item, armor stand, primed TNT, minecart,
// falling block, orb, bobber) that ended a fall on the ground, noted under
// its lock and answered once the lock is down.
type bodyLanding struct {
	w         *world.World
	x, y, z   float64
	halfWidth float64
	// dampens is set for an item whose stack dampens vibrations.
	dampens bool
	// stand is an armor stand, whose landing is heard (LivingEntity.
	// causeFallDamage); fall is the fall that ended.
	stand  bool
	silent bool
	fall   float64
}

// standSafeFall is an armor stand's safe fall distance.
const standSafeFall = 3

// runBodyLandings plays an armor stand's landing sounds and posts each body's
// hit-ground game event with the block it stands on.
// vanilla: Entity.checkFallDamage, LivingEntity.causeFallDamage,
// ArmorStand.getFallSounds
func (s *Server) runBodyLandings(landings []bodyLanding) {
	for _, l := range landings {
		sx, sy, sz := rules.StandingBlock(l.w, l.x, l.y, l.z, l.halfWidth)
		affected := l.w.BlockAt(sx, sy, sz)
		if l.stand && !l.silent {
			eff := rules.FallOn(affected)
			if rules.FallDamage(l.fall*eff.Scale+eff.Offset, standSafeFall, eff.Modifier) > 0 {
				s.broadcastSoundID(l.w, registry.SoundEntityArmorStandFall, protocol.SoundCategoryNeutral, l.x, l.y, l.z, 1, 1)
				if fall, volume, pitch, ok := blockFallSound(l.w, l.x, l.y, l.z); ok {
					s.broadcastSoundID(l.w, fall, protocol.SoundCategoryNeutral, l.x, l.y, l.z, volume, pitch)
				}
			}
		}
		if l.stand {
			// Not gated by silence. vanilla: LivingEntity.checkFallDamage
			if count := rules.LandingParticleCount(l.fall, standSafeFall); count > 0 {
				ox, _, oz, state := rules.OnPosCell(l.w, l.x, l.y, l.z, l.halfWidth)
				if registry.BlockOf(state) != registry.BlockAir {
					x, z := rules.LandingParticleXZ(l.x, l.z, ox, oz)
					s.sendBlockParticles(l.w, protocol.ParticleBlock, state, x, l.y, z, 0, 0, 0, 0.15, count)
				}
			}
		}
		s.gameEvent(l.w, rules.GameEventHitGround, l.x, l.y, l.z,
			rules.GameEventContext{Dampens: l.dampens, Affected: affected})
	}
}
