package entity

import (
	"math"

	"github.com/golem-mc/golem/internal/server/rules"
)

// The end crystal's numbers, from the EntityTypes.END_CRYSTAL registration:
// sized(2.0, 2.0), clientTrackingRange(16), updateInterval(Integer.MAX_VALUE).
const (
	EndCrystalSize          = 2.0
	EndCrystalTrackingRange = 16
	// EndCrystalBlastPower is the power of the explosion a hurt sets off.
	// vanilla: EndCrystal.hurtServer
	EndCrystalBlastPower = 6.0
)

// EndCrystal is one end crystal: it never moves and has no health, any hurt
// removes it.
type EndCrystal struct {
	ID int32
	Place
	// VX, VY and VZ are retained for block effects and saving, but never integrated.
	VX, VY, VZ   float64
	FallDistance float64
	// ShowBottom is whether the bedrock slab under the crystal is drawn; a
	// placed crystal hides it.
	ShowBottom   bool
	Invulnerable bool
	// Beam is the block the crystal's beam points at, when set.
	Beam *[3]int32
	Dead bool
	// Silent mutes the blast's sound. vanilla: Entity.isSilent
	Silent bool
}

// NewEndCrystal is a crystal standing at the given feet position.
func NewEndCrystal(id int32, x, y, z float64) *EndCrystal {
	return &EndCrystal{ID: id, X: x, Y: y, Z: z, ShowBottom: true}
}

// CapFall is Entity.checkFallDistanceAccumulation.
func (c *EndCrystal) CapFall() { CapFallAccumulation(c.VY, &c.FallDistance) }

// BlastBox is the crystal's hit box.
func (c *EndCrystal) BlastBox() (minX, minY, minZ, maxX, maxY, maxZ float64) {
	half := EndCrystalSize / 2
	return c.X - half, c.Y, c.Z - half, c.X + half, c.Y + EndCrystalSize, c.Z + half
}

// EndCrystalPlacementBox is the cell above the clicked block and the one
// over it, which no body may overlap.
// vanilla: EndCrystalItem.useOn
func EndCrystalPlacementBox(x, y, z int32) AABB {
	return AABB{float64(x), float64(y), float64(z), float64(x) + 1, float64(y) + 2, float64(z) + 1}
}

// HasBeam reports whether the beam target differs from its default, empty.
// vanilla: ServerEntity.sendPairingData sends only the non-default fields
func (c *EndCrystal) HasBeam() bool { return c.Beam != nil }

// HidesBottom reports whether show-bottom differs from its default, true.
func (c *EndCrystal) HidesBottom() bool { return !c.ShowBottom }

// FireCell is the cell the crystal lights fire in.
// vanilla: EndCrystal.tick
func (c *EndCrystal) FireCell() (x, y, z int32) {
	return int32(math.Floor(c.X)), int32(math.Floor(c.Y)), int32(math.Floor(c.Z))
}

// Overlaps reports whether the crystal's hit box crosses the box.
func (c *EndCrystal) Overlaps(box AABB) bool {
	return box.Crosses(NewAABB(c.BlastBox()))
}

// ReachedByBlast reports whether a blast at (x, y, z) of the given radius
// removes the crystal: a blast that may not hurt entities leaves it.
// vanilla: ServerExplosion.hurtEntities, EndCrystal.hurtServer
func (c *EndCrystal) ReachedByBlast(x, y, z, radius float64, harmless bool) bool {
	return !harmless && !c.Invulnerable && !c.Dead && rules.ExplosionInBlastRange(math.Sqrt(Dist2(x, y, z, c.X, c.Y, c.Z)), radius)
}

// EndCrystalCanStandOn reports whether the item may stand a crystal on the base
// block with the block above it.
// vanilla: EndCrystalItem.useOn
func EndCrystalCanStandOn(base, above uint16) bool {
	return rules.EndCrystalBase(base) && rules.IsAir(above)
}
