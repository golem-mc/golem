package rules

import "github.com/golem-mc/golem/registry"

// CrossConnections reports which horizontal neighbours a fence or bars at
// (x, y, z) connects to: fences by ConnectsToFence (same group, walls, gates
// from the side, sturdy faces), bars by ConnectsToBars (bars, walls, sturdy
// faces).
// vanilla: FenceBlock.connectsTo, IronBarsBlock.attachsTo
func CrossConnections(w BlockReader, b registry.Block, x, y, z int32) (north, south, east, west bool) {
	north = CrossConnects(w, b, x, y, z, "north")
	south = CrossConnects(w, b, x, y, z, "south")
	east = CrossConnects(w, b, x, y, z, "east")
	west = CrossConnects(w, b, x, y, z, "west")
	return north, south, east, west
}

// CrossConnects reports whether the fence or bars at (x, y, z) connect to the
// neighbour on the side named dir.
func CrossConnects(w BlockReader, b registry.Block, x, y, z int32, dir string) bool {
	dx, dz := registry.SideOffset(dir)
	state := w.BlockAt(x+dx, y, z+dz)
	if b.IsBars() {
		return registry.ConnectsToBars(state, dir)
	}
	return registry.ConnectsToFence(b, state, dir)
}

// sidePropOf is the connection property of the side named dir; like
// registry.SideOffset, anything but north, south or east reads as west.
func sidePropOf(dir string) registry.Prop {
	switch dir {
	case "north":
		return registry.PropNorth
	case "south":
		return registry.PropSouth
	case "east":
		return registry.PropEast
	}
	return registry.PropWest
}

// CrossStateWithConnections returns the block state a CrossCollisionBlock of
// the given block should be placed in, given the four horizontal neighbours
// around (x, y, z). The default state's other properties are preserved.
func CrossStateWithConnections(w BlockReader, b registry.Block, x, y, z int32) uint16 {
	return CrossStateFromState(w, b, b.DefaultState(), x, y, z)
}

// CrossStateFromState returns the block state the CrossCollisionBlock b
// takes at (x, y, z) with its four connections recomputed from the
// neighbours; base's other properties, waterlogged among them, are kept.
func CrossStateFromState(w BlockReader, b registry.Block, base uint16, x, y, z int32) uint16 {
	north, south, east, west := CrossConnections(w, b, x, y, z)
	if registry.BlockOf(base) == b && registry.StateBool(base, registry.PropNorth) == north &&
		registry.StateBool(base, registry.PropSouth) == south &&
		registry.StateBool(base, registry.PropEast) == east &&
		registry.StateBool(base, registry.PropWest) == west {
		return base
	}
	base, _ = registry.WithBool(base, registry.PropNorth, north)
	base, _ = registry.WithBool(base, registry.PropSouth, south)
	base, _ = registry.WithBool(base, registry.PropEast, east)
	base, _ = registry.WithBool(base, registry.PropWest, west)
	return base
}
