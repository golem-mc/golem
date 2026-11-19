package rules

import (
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// DoublePlantPlacement turns the chosen lower half into both halves, refused
// unless the cell above is below maxY (the top block) and replaceable. The
// small dripleaf waterlogs each half by its cell. The pitcher crop is never
// placed this way: a pod plants a one-tall age-0 crop.
// vanilla: DoublePlantBlock.getStateForPlacement, setPlacedBy, copyWaterloggedFrom
func DoublePlantPlacement(w BlockReader, lower uint16, x, y, z, maxY int32) (placed, upper uint16, ok bool) {
	if y >= maxY || !IsReplaceableState(w.BlockAt(x, y+1, z)) {
		return 0, 0, false
	}
	placedState := halfWithWater(lower, "lower", world.HoldsWater(w.BlockAt(x, y, z)))
	return placedState, DoublePlantUpper(w, lower, x, y, z), true
}

// DoublePlantUpper is the upper half for the final lower half: its
// properties with half=upper, waterlogged from the cell above. Only the small
// dripleaf also copies facing.
// vanilla: DoublePlantBlock.setPlacedBy, copyWaterloggedFrom, SmallDripleafBlock.setPlacedBy
func DoublePlantUpper(w BlockReader, lower uint16, x, y, z int32) uint16 {
	return halfWithWater(lower, "upper", world.HoldsWater(w.BlockAt(x, y+1, z)))
}

// halfWithWater sets half, and waterlogged where the block has it.
func halfWithWater(state uint16, half string, water bool) uint16 {
	state, _ = registry.WithProp(state, registry.PropHalf, half)
	if registry.StateHas(state, registry.PropWaterlogged) {
		state, _ = registry.WithBool(state, registry.PropWaterlogged, water)
	}
	return state
}
