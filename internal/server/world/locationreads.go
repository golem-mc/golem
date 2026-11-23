package world

import (
	"github.com/golem-mc/golem/registry"
)

// CanSeeSky reports whether the cell holds full sky light.
// vanilla: BlockAndLightGetter.canSeeSky
func (w *World) CanSeeSky(x, y, z int32) bool {
	return w.SkyLightAt(x, y, z) >= 15
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

// SmokeyAt reports whether campfire smoke reaches the block.
func (w *World) SmokeyAt(x, y, z int32) bool {
	return SmokeyColumn(w.BlockAt, x, y, z)
}

// SmokeyColumn reports whether a lit campfire sits within five blocks below
// (x, y, z), or just under the first block below that fills the smoke column.
// Cells are read in order and only as far as needed.
// vanilla: CampfireBlock.isSmokeyPos
func SmokeyColumn(blockAt func(x, y, z int32) uint16, x, y, z int32) bool {
	for i := int32(1); i <= 5; i++ {
		state := blockAt(x, y-i, z)
		if registry.IsLitCampfire(state) {
			return true
		}
		if smokeBlocked(state) {
			return registry.IsLitCampfire(blockAt(x, y-i-1, z))
		}
	}
	return false
}

// smokeBlocked reports whether the state's collision meets the 4-wide post
// the smoke rises through.
// vanilla: CampfireBlock.SHAPE_VIRTUAL_POST
func smokeBlocked(state uint16) bool {
	for _, b := range CollisionBoxes(state) {
		if b.MinX < 10.0/16 && b.MaxX > 6.0/16 && b.MinZ < 10.0/16 && b.MaxZ > 6.0/16 && b.MaxY > b.MinY {
			return true
		}
	}
	return false
}
