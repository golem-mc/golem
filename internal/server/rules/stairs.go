package rules

import "github.com/golem-mc/golem/registry"

// StairsStateForPlacement returns the state a stair placed at (x, y, z) takes:
// the waterlogged property from the fluid in the cell it fills, then the corner
// shape from the stairs beside it.
// vanilla: StairBlock.getStateForPlacement
func StairsStateForPlacement(w BlockReader, b registry.Block, placed uint16, x, y, z int32) uint16 {
	if !b.IsStairs() {
		return placed
	}
	placed, _ = registry.WithBool(placed, registry.PropWaterlogged, waterAt(w, x, y, z))
	return StairsStateWithShape(w, b, placed, x, y, z)
}

// StairsStateWithShape sets the corner shape from the stairs beside it,
// run on placement and on every horizontal neighbour change. Non-stairs are
// unchanged.
func StairsStateWithShape(w BlockReader, b registry.Block, placed uint16, x, y, z int32) uint16 {
	return registry.StairsStateWithShape(w, b, placed, x, y, z)
}
