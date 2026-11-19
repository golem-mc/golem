package rules

import "github.com/golem-mc/golem/registry"

// FenceGateStateInWall returns the fence gate state with in_wall set from the
// walls beside it, on the axis across its facing. A block that is not a gate
// is returned unchanged.
// vanilla: FenceGateBlock.getStateForPlacement, FenceGateBlock.updateShape
func FenceGateStateInWall(w BlockReader, b registry.Block, state uint16, x, y, z int32) uint16 {
	if !b.IsFenceGate() {
		return state
	}
	var dx, dz int32
	switch registry.StateValue(state, registry.PropFacing) {
	case "north", "south":
		dx = 1
	case "east", "west":
		dz = 1
	default:
		return state
	}
	inWall := isWallAt(w, x+dx, y, z+dz) || isWallAt(w, x-dx, y, z-dz)
	if registry.StateBool(state, registry.PropInWall) == inWall {
		return state
	}
	state, _ = registry.WithBool(state, registry.PropInWall, inWall)
	return state
}

func isWallAt(w BlockReader, x, y, z int32) bool {
	return registry.BlockOf(w.BlockAt(x, y, z)).IsWall()
}
