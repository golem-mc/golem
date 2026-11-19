package entity

import (
	"math"

	"github.com/golem-mc/golem/internal/server/rules"
)

// stuckFactor is the velocity multiplier Entity.makeStuckInBlock left for
// the next move; the zero value means none.
type stuckFactor [3]float64

// scale multiplies a move by the factor and reports whether one was set.
// The caller zeroes the velocity, as Entity.move does once the multiplier
// is spent.
// vanilla: Entity.move
func (s *stuckFactor) scale(dx, dy, dz float64) (float64, float64, float64, bool) {
	if s[0]*s[0]+s[1]*s[1]+s[2]*s[2] <= 1.0e-7 {
		*s = stuckFactor{}
		return dx, dy, dz, false
	}
	f := *s
	*s = stuckFactor{}
	return dx * f[0], dy * f[1], dz * f[2], true
}

// blockSpeedFactor is Entity.getBlockSpeedFactor for feet at (x, feetY, z):
// the block it stands in, or the one below when that is ordinary. Water and
// bubble columns keep their own factor; a waterlogged block or water plant
// defers.
// vanilla: Entity.getBlockSpeedFactor
func blockSpeedFactor(w World, x, feetY, z float64) float64 {
	state := w.BlockAt(int32(math.Floor(x)), int32(math.Floor(feetY)), int32(math.Floor(z)))
	here := rules.BlockSpeedFactor(state)
	if rules.IsWaterBlock(state) || rules.IsBubbleColumn(state) || here != 1 {
		return here
	}
	return rules.BlockSpeedFactor(blockBelowFeet(w, x, feetY, z))
}

// blockJumpFactor is Entity.getBlockJumpFactor, choosing the block as
// blockSpeedFactor does.
// vanilla: Entity.getBlockJumpFactor
func blockJumpFactor(w World, x, feetY, z float64) float64 {
	here := rules.BlockJumpFactor(w.BlockAt(int32(math.Floor(x)), int32(math.Floor(feetY)), int32(math.Floor(z))))
	if here != 1 {
		return here
	}
	return rules.BlockJumpFactor(blockBelowFeet(w, x, feetY, z))
}

// blockBelowFeet is the block Entity.getBlockPosBelowThatAffectsMyMovement
// names.
func blockBelowFeet(w World, x, feetY, z float64) uint16 {
	return w.BlockAt(int32(math.Floor(x)), int32(math.Floor(feetY-0.500001)), int32(math.Floor(z)))
}

// stuckIn is the multiplier left by the last of the box's cells to call
// makeStuckInBlock. Powder snow counts for a living body only at the feet;
// skipWeb and skipBerry are for kinds that ignore cobwebs and berry bushes.
// vanilla: WebBlock.entityInside, SweetBerryBushBlock.entityInside,
// PowderSnowBlock.entityInside
func stuckIn(w World, box AABB, x, feetY, z float64, living, weaving, skipWeb, skipBerry bool) stuckFactor {
	var out stuckFactor
	fx, fy, fz := int32(math.Floor(x)), int32(math.Floor(feetY)), int32(math.Floor(z))
	box.insideCells(func(cx, cy, cz int32) {
		state := w.BlockAt(cx, cy, cz)
		if living && rules.IsPowderSnow(state) && (cx != fx || cy != fy || cz != fz) {
			return
		}
		mx, my, mz, ok := rules.StuckMultiplier(state, living, weaving)
		if !ok || skipWeb && rules.IsCobweb(state) || skipBerry && rules.IsSweetBerryBush(state) {
			return
		}
		out = stuckFactor{mx, my, mz}
	})
	return out
}

// insideNonLiving is one cell's share of a non-living body's block effects
// that the web and the snow make: the multiplier it leaves in stuck for the
// next move, and whether a snowflake may rise above it, the body's known
// movement being sideways. A non-living body need not stand in the snow; the
// caller rolls the level random's even odds per flake.
// vanilla: WebBlock.entityInside, PowderSnowBlock.entityInside
func insideNonLiving(state uint16, sideways bool, stuck *stuckFactor) (flake bool) {
	if mx, my, mz, ok := rules.StuckMultiplier(state, false, false); ok {
		*stuck = stuckFactor{mx, my, mz}
	}
	return sideways && rules.IsPowderSnow(state)
}
