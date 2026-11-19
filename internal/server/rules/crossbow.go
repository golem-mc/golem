package rules

import (
	"github.com/golem-mc/golem/registry"

	"github.com/golem-mc/golem/internal/protocol"
)

const (
	// CrossbowArrowPower is the speed a loaded arrow leaves at.
	// vanilla: CrossbowItem.ARROW_POWER
	CrossbowArrowPower = 3.15

	// CrossbowFireworkPower is the speed a volley holding a rocket leaves at.
	// vanilla: CrossbowItem.FIREWORK_POWER
	CrossbowFireworkPower = 1.6
)

// CrossbowShotPower is the speed a volley leaves at: a rocket anywhere in it
// slows every projectile.
// vanilla: CrossbowItem.getShootingPower
func CrossbowShotPower(anyFirework bool) float64 {
	if anyFirework {
		return CrossbowFireworkPower
	}
	return CrossbowArrowPower
}

// CrossbowWearPerShot is the durability one fired projectile costs.
// vanilla: CrossbowItem.getDurabilityUse
func CrossbowWearPerShot(firework bool) int32 {
	if firework {
		return 3
	}
	return 1
}

// CrossbowChargeTicks is how many ticks of use load the crossbow.
// vanilla: CrossbowItem.getChargeDuration
func CrossbowChargeTicks(crossbow protocol.ItemStack, ctx ...*EnchContext) int32 {
	value := unfilteredEnchValue(crossbow, ctx, func(e *registry.EnchantmentEffects) *registry.EnchEffect { return e.CrossbowChargeTime }, 1.25)
	return int32(float32(max(0, value)) * 20)
}

// CrossbowChargeStep reports what a load of duration ticks does at elapsed:
// start sound at a fifth, middle at a half, load at the end. Each sound plays
// on the first tick reaching it; the load is asked every tick after, so a
// load that found no arrow retries. Duration 0 reaches all on tick one.
// vanilla: CrossbowItem.onUseTick
func CrossbowChargeStep(elapsed, duration int32) (start, mid, load bool) {
	share := func(e int32) float32 { return float32(e) / float32(duration) }
	first := func(percent float32) bool {
		return share(elapsed) >= percent && !(share(elapsed-1) >= percent)
	}
	return first(0.2), first(0.5), share(elapsed) >= 1
}

// CrossbowProjectileCount is how many arrows one load draws.
// vanilla: EnchantmentHelper.processProjectileCount
func CrossbowProjectileCount(crossbow protocol.ItemStack, ctx ...*EnchContext) int {
	value := itemEnchValue(crossbow, ctx, func(e *registry.EnchantmentEffects) []registry.EnchConditional { return e.ProjectileCount }, 1)
	return max(0, int(value))
}

// CrossbowMaxSpread is the angle in degrees between the outermost arrows of a
// volley and the middle line, scaled by the volley's size below.
// vanilla: EnchantmentHelper.processProjectileSpread
func CrossbowMaxSpread(crossbow protocol.ItemStack, ctx ...*EnchContext) float32 {
	value := itemEnchValue(crossbow, ctx, func(e *registry.EnchantmentEffects) []registry.EnchConditional { return e.ProjectileSpread }, 0)
	return float32(max(0, value))
}

// CrossbowAngles is the angle in degrees each of n arrows turns from the aim,
// in the order they are shot: the first straight on when n is odd, the rest
// alternating sides.
// vanilla: ProjectileWeaponItem.shoot
func CrossbowAngles(n int, maxAngle float32) []float32 {
	angles := make([]float32, n)
	var step float32
	if n > 1 {
		step = 2 * maxAngle / float32(n-1)
	}
	offset := float32((n-1)%2) * step / 2
	direction := float32(1)
	for i := range n {
		angles[i] = offset + direction*float32((i+1)/2)*step
		direction = -direction
	}
	return angles
}

// PiercingLevel is the number of bodies past the first an arrow shot from the
// weapon passes through.
// vanilla: EnchantmentHelper.getPiercingCount
func PiercingLevel(weapon, ammo protocol.ItemStack, contexts ...*EnchContext) int32 {
	ctx := EnchContext{Tool: ammo, Victim: -1, Attacker: -1, DirectAttacker: -1}
	if len(contexts) > 0 && contexts[0] != nil {
		ctx.Rand = contexts[0].Rand
	}
	value := itemEnchValue(weapon, []*EnchContext{&ctx}, func(e *registry.EnchantmentEffects) []registry.EnchConditional { return e.ProjectilePiercing }, 0)
	return max(0, int32(value))
}
