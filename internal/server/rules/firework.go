package rules

import (
	"math"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/registry"
)

// A firework rocket's burst: the damage the stars ask, the radius, and the
// falloff a body at dist takes, over the burst's sight lines. Which bodies
// and the hurt itself are the server's.
// vanilla: FireworkRocketEntity.dealExplosionDamage

// The burst's own numbers, from the dealExplosionDamage bytecode: the
// radius of five blocks, the base of five a burst asks plus two a star,
// and the falloff's square root of the distance's share of the radius.
const (
	FireworkBurstRadius  = 5.0
	fireworkBurstBase    = 5.0
	fireworkBurstPerStar = 2.0
)

// FireworkBurstDamage is what a burst of n explosions asks of the bodies
// its radius reaches: five and two a star, nothing for a rocket that
// carried no stars at all.
func FireworkBurstDamage(explosions int) float64 {
	if explosions <= 0 {
		return 0
	}
	return fireworkBurstBase + float64(explosions)*fireworkBurstPerStar
}

// FireworkBurstInBurstRange reports whether a body dist blocks from the
// burst is one it reaches at all: distanceToSqr's twenty-five the vanilla
// walk skips past.
func FireworkBurstInBurstRange(dist float64) bool {
	return dist <= FireworkBurstRadius
}

// FireworkBurstFalloffDamage is amount times the square root of the share of
// the radius dist leaves; the caller has put the body in range.
func FireworkBurstFalloffDamage(amount, dist float64) float64 {
	return amount * math.Sqrt((FireworkBurstRadius-dist)/FireworkBurstRadius)
}

// FireworkBurstVisible reports whether the burst sees the box: a clear clip
// from the rocket to its feet, or else to half its height.
// vanilla: FireworkRocketEntity.dealExplosionDamage
func FireworkBurstVisible(w BlockReader, fromX, fromY, fromZ,
	minX, minY, minZ, maxX, maxY, maxZ float64) bool {
	x := (minX + maxX) / 2
	z := (minZ + maxZ) / 2
	for step := 0.0; step < 2; step++ {
		if !sightBlocked(w, fromX, fromY, fromZ, x, minY+(maxY-minY)*0.5*step, z) {
			return true
		}
	}
	return false
}

// GliderUsable reports a glider worn in slot (an EquipmentSlots name), the
// slot its equippable names, that is not one wear from breaking.
// vanilla: LivingEntity.canGlideUsing, ItemStack.nextDamageWillBreak
func GliderUsable(stack protocol.ItemStack, slot string) bool {
	if stack.Empty() || !registry.StackGlider(stack) {
		return false
	}
	if eq, ok := registry.StackEquippable(stack); !ok || eq.Slot != slot {
		return false
	}
	maxDamage := registry.StackMaxDamage(stack)
	return maxDamage == 0 || stack.Unbreakable || stack.Damage+1 < maxDamage
}
