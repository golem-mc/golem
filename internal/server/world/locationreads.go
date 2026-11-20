package world

import (
	"github.com/golem-mc/golem/registry"
)

// CanSeeSky reports whether no block at or above the column's top stands
// over y.
// vanilla: LevelReader.canSeeSky
func (w *World) CanSeeSky(x, y, z int32) bool {
	return y > w.SurfaceHeight(x, z)
}

// FluidAt is the fluid state at a block as a fluid predicate reads it: the
// fluid's registry name ("minecraft:empty" for none), its amount (the level
// property of a flowing fluid, 1-8) and whether it falls. A source has no
// level property.
// vanilla: FluidState, FlowingFluid.LEVEL, FlowingFluid.FALLING
func (w *World) FluidAt(x, y, z int32) (id string, amount int, falling bool) {
	fs := fluidHolding(w.BlockAt(x, y, z))
	switch {
	case fs.kind == fluidWater && fs.isSource():
		id = "minecraft:water"
	case fs.kind == fluidWater:
		id = "minecraft:flowing_water"
	case fs.kind == fluidLava && fs.isSource():
		id = "minecraft:lava"
	case fs.kind == fluidLava:
		id = "minecraft:flowing_lava"
	default:
		id = "minecraft:empty"
	}
	return id, fs.amount, fs.falling
}

// SmokeyAt reports whether campfire smoke reaches the block: a lit campfire
// within five blocks below, or just under the first block below that fills
// the smoke column.
// vanilla: CampfireBlock.isSmokeyPos
func (w *World) SmokeyAt(x, y, z int32) bool {
	for i := int32(1); i <= 5; i++ {
		state := w.BlockAt(x, y-i, z)
		if registry.IsLitCampfire(state) {
			return true
		}
		// SHAPE_VIRTUAL_POST is the column from 6/16 to 10/16 on x and z.
		for _, b := range CollisionBoxes(state) {
			if b.MinX < 10.0/16 && b.MaxX > 6.0/16 && b.MinZ < 10.0/16 && b.MaxZ > 6.0/16 && b.MaxY > b.MinY {
				return registry.IsLitCampfire(w.BlockAt(x, y-i-1, z))
			}
		}
	}
	return false
}
