package world

import "github.com/golem-mc/golem/registry"

// RemovalLeaves is the block a removal leaves in a cell: the water a
// waterlogged block or a seagrass held, otherwise air.
// vanilla: Level.removeBlock, fluidState.createLegacyBlock()
func RemovalLeaves(state uint16) uint16 {
	return fluidHolding(state).createLegacy()
}

// HoldsWater reports whether a state's fluid is water, at any level.
// vanilla: LevelReader.isWaterAt
func HoldsWater(state uint16) bool {
	return fluidHolding(state).kind == fluidWater
}

// HoldsFluid reports any fluid: water and lava blocks, waterlogged blocks,
// kelp, seagrass and bubble columns.
// vanilla: BlockState.getFluidState().isEmpty
func HoldsFluid(state uint16) bool {
	return fluidHolding(state).kind != fluidNone
}

// HoldsFluidSource reports a source (full, not falling). Waterlogged blocks,
// kelp, seagrass and bubble columns answer for their own cell, which a fluid
// clip stops at.
// vanilla: FluidState.isSource
func HoldsFluidSource(state uint16) bool {
	return fluidHolding(state).isSource()
}

// IsLowerHalfBelow reports whether the block under (x, y, z) is the lower
// half of the door or two-tall plant.
func IsLowerHalfBelow(w BlockReader, block registry.Block, x, y, z int32) bool {
	below := w.BlockAt(x, y-1, z)
	return registry.BlockOf(below) == block && registry.StateValue(below, registry.PropHalf) == "lower"
}

// isDoublePlantState reports a two-tall plant half for block b at age; the
// pitcher crop is one from age 3.
// vanilla: PitcherCropBlock.updateShape, isDouble
func isDoublePlantState(b registry.Block, age int) bool {
	if !b.IsDoublePlant() {
		return false
	}
	if b == registry.BlockPitcherCrop {
		return age >= 3
	}
	return true
}

// DoublePlantSurvives reports whether the half state survives at (x, y, z).
// Placement asks before the cell is written, so the small dripleaf reads the
// old fluid then and its own waterlogging after.
// vanilla: DoublePlantBlock.canSurvive and the subclasses' mayPlaceOn
func (w *World) DoublePlantSurvives(state uint16, x, y, z int32) bool {
	return doublePlantSurvives(w, state, x, y, z)
}

func doublePlantSurvives(w BlockReader, state uint16, x, y, z int32) bool {
	block := registry.BlockOf(state)
	if registry.StateValue(state, registry.PropHalf) == "upper" {
		return IsLowerHalfBelow(w, block, x, y, z)
	}
	belowState := w.BlockAt(x, y-1, z)
	if int(belowState) >= registry.StateCount {
		return false
	}
	below := registry.BlockOf(belowState)
	switch block {
	case registry.BlockSmallDripleaf:
		if below.InTag(registry.TagIDSupportsSmallDripleaf) {
			return true
		}
		fs := fluidHolding(w.BlockAt(x, y, z))
		return fs.kind == fluidWater && fs.isSource() && below.InTag(registry.TagIDSupportsVegetation)
	case registry.BlockTallSeagrass:
		// The water TallSeagrassBlock.canSurvive also asks for is its own.
		return registry.FaceSturdy(belowState, dirUp, registry.SupportFull) &&
			!below.InTag(registry.TagIDCannotSupportSeagrass)
	case registry.BlockPitcherCrop:
		return rawLightAt(w, x, y, z) >= 8 && below.InTag(registry.TagIDSupportsCrops)
	}
	return below.InTag(registry.TagIDSupportsVegetation)
}

// doublePlantUpdate answers a two-tall plant's neighbour change, reporting
// whether it was one: a half whose partner is gone breaks, as does one that
// cannot survive, for an update from any direction.
// vanilla: DoublePlantBlock.updateShape
func (p *Physics) doublePlantUpdate(x, y, z int32, state uint16, placed bool, from [3]int32, changes *[]BlockChange) bool {
	block := registry.BlockOf(state)
	if !block.IsDoublePlant() {
		return false
	}
	if !isDoublePlantState(block, stateIntOr(state, registry.PropAge, 0)) {
		return false
	}
	if placed {
		return true
	}
	half := registry.StateValue(state, registry.PropHalf)
	lower := half == "lower"
	if from[0] == 0 && from[2] == 0 && lower == (from[1] == 1) {
		partner := p.w.BlockAt(x, y+from[1], z)
		if registry.BlockOf(partner) != block || registry.StateValue(partner, registry.PropHalf) == half {
			p.breakWithDrop(x, y, z, changes)
		}
		return true
	}
	// Any other update falls through to VegetationBlock.updateShape.
	if !p.w.DoublePlantSurvives(state, x, y, z) {
		p.breakWithDrop(x, y, z, changes)
	}
	return true
}
