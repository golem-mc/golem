package rules

import "github.com/golem-mc/golem/registry"

// WallConnections reports which horizontal neighbours a wall connects to.
func WallConnections(w BlockReader, x, y, z int32) (north, south, east, west bool) {
	return registry.WallConnections(w, x, y, z)
}

// WallStateWithConnections is the wall's placed state from its neighbours
// and the block above.
func WallStateWithConnections(w BlockReader, b registry.Block, x, y, z int32) uint16 {
	return registry.WallStateWithConnections(w, b, x, y, z)
}

// WallStateFromState returns the state of wall b at (x, y, z) with its sides
// and post recomputed from the neighbours.
func WallStateFromState(w BlockReader, b registry.Block, base uint16, x, y, z int32) uint16 {
	return registry.WallStateFromState(w, b, base, x, y, z)
}

// WallStateForNeighbor returns the wall state after the cell on the facing
// side changed.
func WallStateForNeighbor(w BlockReader, b registry.Block, base uint16, x, y, z int32, facing string) uint16 {
	return registry.WallStateForNeighbor(w, b, base, x, y, z, facing)
}

// WallStateFromConnections builds the wall state from the four connections
// and the block above.
func WallStateFromConnections(b registry.Block, north, south, east, west bool, above uint16) uint16 {
	return registry.WallStateFromConnections(b, north, south, east, west, above)
}
