package rules

import (
	"math"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/registry"
)

// The mace's smash attack: a blow struck while falling, which adds the fall
// to the damage, pushes the bodies around the victim away and spends the
// attacker's fall. vanilla: MaceItem

const (
	// smashFallThreshold is the fall, in blocks, a blow must be struck past
	// to smash. vanilla: MaceItem.SMASH_ATTACK_FALL_THRESHOLD
	smashFallThreshold = 1.5
	// smashHeavyThreshold is the fall past which the smash is a heavy one:
	// the heavy ground sound and twice the push.
	// vanilla: MaceItem.SMASH_ATTACK_HEAVY_THRESHOLD
	smashHeavyThreshold = 5.0
	// SmashKnockbackRadius is how far from the victim, in blocks, a smash
	// pushes the bodies around it. vanilla: MaceItem.SMASH_ATTACK_KNOCKBACK_RADIUS
	SmashKnockbackRadius = 3.5
	// SmashParticles is the data of the smash's world event, the count of
	// the dust particles it raises. vanilla: MaceItem.knockback
	SmashParticles = 750
)

// smashKnockbackPower is the push per block inside the radius, the float
// 0.7f widened. vanilla: MaceItem.SMASH_ATTACK_KNOCKBACK_POWER
var smashKnockbackPower = float64(float32(0.7))

// SmashLift is the upward push a smash gives every body it shoves, whatever
// the distance: Entity.push's y of 0.7f.
var SmashLift = float64(float32(0.7))

// SmashAttackerLift is the vertical motion the smash sets on its attacker,
// the 0.01f that holds the body for the bounce. vanilla: MaceItem.hurtEnemy
var SmashAttackerLift = float64(float32(0.01))

// CanSmashAttack reports whether a blow with held, struck after falling
// fallDistance blocks, smashes: a mace, past the threshold, not gliding.
// vanilla: MaceItem.canSmashAttack
func CanSmashAttack(held protocol.ItemStack, fallDistance float64, gliding bool) bool {
	return isMace(held) && fallDistance > smashFallThreshold && !gliding
}

func isMace(held protocol.ItemStack) bool {
	if held.Empty() {
		return false
	}
	return registry.Item(held.ItemID) == registry.ItemMace
}

// SmashDamageBonus is the damage a smash adds to the blow for a fall of
// fallDistance blocks: four a block for the first three, two a block to
// eight, one a block past it, plus perBlock (density's) for every block.
// vanilla: MaceItem.getAttackDamageBonus
func SmashDamageBonus(fallDistance, perBlock float64) float64 {
	var damage float64
	switch {
	case fallDistance <= 3:
		damage = 4 * fallDistance
	case fallDistance <= 8:
		damage = 12 + 2*(fallDistance-3)
	default:
		damage = 22 + fallDistance - 8
	}
	return damage + perBlock*fallDistance
}

// HeavySmash reports whether a smash after fallDistance blocks is a heavy
// one. vanilla: MaceItem.hurtEnemy, getKnockbackPower
func HeavySmash(fallDistance float64) bool {
	return fallDistance > smashHeavyThreshold
}

// SmashKnockback is the push on a body at (dx, dy, dz) from the victim:
// strength falling linearly to 0 at the radius, doubled for a heavy smash,
// cut by knockback resistance, along the horizontal unit offset, with
// SmashLift up. false past the radius or with nothing left.
// vanilla: MaceItem.knockback, getKnockbackPower
func SmashKnockback(dx, dy, dz, fallDistance, knockbackResistance float64) (vx, vy, vz float64, ok bool) {
	dist := math.Sqrt(dx*dx + dy*dy + dz*dz)
	if dist > SmashKnockbackRadius {
		return 0, 0, 0, false
	}
	power := (SmashKnockbackRadius - dist) * smashKnockbackPower * (1 - knockbackResistance)
	if HeavySmash(fallDistance) {
		power *= 2
	}
	if power <= 0 {
		return 0, 0, 0, false
	}
	// Vec3.normalize leaves an offset under 1e-5f long as the zero vector.
	if dist < float64(float32(1.0e-5)) {
		return 0, SmashLift, 0, true
	}
	return dx / dist * power, SmashLift, dz / dist * power, true
}
