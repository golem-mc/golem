package rules

import (
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// ChorusPlantPlacementState is ChorusPlantBlock.getStateForPlacement: the
// placed plant takes the connections of its neighbours.
// vanilla: ChorusPlantBlock.getStateForPlacement
func ChorusPlantPlacementState(w BlockReader, block registry.Block, x, y, z int32) (uint16, bool) {
	if block != registry.BlockChorusPlant {
		return 0, false
	}
	return world.ChorusPlantConnections(w.BlockAt, x, y, z)
}
