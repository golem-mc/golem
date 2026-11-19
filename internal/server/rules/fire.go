package rules

import (
	"math"

	"github.com/golem-mc/golem/internal/server/blocklogic"
	"github.com/golem-mc/golem/registry"
)

// FireSurvivesAt reports whether a cell can hold fire.
func FireSurvivesAt(w FireWorld, x, y, z int32) bool {
	floor := w.VoidY()
	return blocklogic.FireSurvivesAt(w, x, y, z, floor, floor)
}

// FireStateForPlacement is the state a fire lit or spread into the cell takes.
// No floor bound applies: placement reads the block below at any height.
func FireStateForPlacement(w FireWorld, x, y, z int32) uint16 {
	return blocklogic.FireStateAt(w, x, y, z, math.MinInt32, 0)
}

// vanilla: FireBlock.updateShape, FireBlock.getStateWithAge
func fireNeighborShapedState(w BlockReader, state uint16, x, y, z int32) uint16 {
	if !blocklogic.FireSurvivesAt(w, x, y, z, math.MinInt32, math.MinInt32) {
		// BlockInput.place keeps the requested state when shaping returns air.
		return state
	}
	age, _ := registry.StateInt(state, registry.PropAge)
	return blocklogic.FireStateAt(w, x, y, z, math.MinInt32, age)
}
