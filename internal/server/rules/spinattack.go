package rules

import (
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/registry"
	"math"
)

// The riptide spin attack's fixed numbers.
// vanilla: TridentItem.releaseUsing (startAutoSpinAttack(20, 8.0f, ...))
const (
	SpinAttackTicks  = 20
	SpinAttackDamage = 8.0
	// SpinAttackBounce scales the delta movement on the hit that ends a spin.
	// vanilla: LivingEntity.checkAutoSpinAttack
	SpinAttackBounce = -0.2
	// SpinAttackGroundLift is the grounded release's immediate upward move.
	// vanilla: TridentItem.releaseUsing (1.1999999f)
	SpinAttackGroundLift = 1.1999999284744263
)

// SpinAttackStrength is the trident's push along the look direction.
// vanilla: EnchantmentHelper.getTridentSpinAttackStrength
func SpinAttackStrength(stack protocol.ItemStack, ctx ...*EnchContext) float64 {
	return unfilteredEnchValue(stack, ctx, func(e *registry.EnchantmentEffects) *registry.EnchEffect { return e.TridentSpinAttackStrength }, 0)
}

// SpinAttackPush is the delta movement a spin adds: the look vector scaled to
// the strength.
// vanilla: TridentItem.releaseUsing
func SpinAttackPush(yaw, pitch float32, strength float64) (x, y, z float64) {
	const rad = math.Pi / 180
	xd := -math.Sin(float64(yaw)*rad) * math.Cos(float64(pitch)*rad)
	yd := -math.Sin(float64(pitch) * rad)
	zd := math.Cos(float64(yaw)*rad) * math.Cos(float64(pitch)*rad)
	s := strength / math.Sqrt(xd*xd+yd*yd+zd*zd)
	return xd * s, yd * s, zd * s
}
