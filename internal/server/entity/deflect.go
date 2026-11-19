package entity

import (
	"math/rand"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/registry"
)

// deflectsProjectile reports whether the body turns a projectile back
// instead of taking its hit: a deflects_projectiles type, except a breeze
// meeting a wind charge.
// vanilla: Entity.deflection, Breeze.deflection
func deflectsProjectile(target *ArrowTarget, windCharge bool) bool {
	if windCharge && target.Type == protocol.EntityTypeBreeze {
		return false
	}
	return registry.EntityType(target.Type).InTag(registry.EntityTypeTagIDDeflectsProjectiles)
}

// reverseDeflection is ProjectileDeflection.REVERSE: reverse and halve the
// movement scaled by power, and turn the yaw by 170 + nextFloat()*20.
func reverseDeflection(vx, vy, vz *float64, yaw *float32, r *rand.Rand, power float64) {
	*yaw += 170 + r.Float32()*20
	*vx *= -power * 0.5
	*vy *= -power * 0.5
	*vz *= -power * 0.5
}
