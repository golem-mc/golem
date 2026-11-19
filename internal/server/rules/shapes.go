package rules

import (
	"math/rand"

	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// IsCrossCollisionBlock reports whether b is a fence or a bars block.
func IsCrossCollisionBlock(b registry.Block) bool { return b.IsCrossCollision() }

// IsWallBlock reports whether b is in the walls tag.
func IsWallBlock(b registry.Block) bool { return b.IsWall() }

// IsStairsBlock reports whether b is a stair block.
func IsStairsBlock(b registry.Block) bool { return b.IsStairs() }

// IsFenceGateBlock reports whether b is a fence gate.
func IsFenceGateBlock(b registry.Block) bool { return b.IsFenceGate() }

// IsFenceBlock reports whether b is in the fences tag.
func IsFenceBlock(b registry.Block) bool { return b.IsFence() }

// ShapeWorld is the slice of the world the shape pass reads and writes. The
// world satisfies it; SetBlock reports whether the write took.
type ShapeWorld interface {
	BlockAt(x, y, z int32) uint16
	SetBlock(x, y, z int32, state uint16) bool
}

// NeighborShapedState returns the neighbour-derived state before command placement.
// vanilla: Block.updateFromNeighbourShapes
func NeighborShapedState(w BlockReader, rng *rand.Rand, state uint16, x, y, z int32) uint16 {
	b := registry.BlockOf(state)
	switch {
	case b == registry.BlockRedstoneWire:
		if s, ok := WireStateFromState(w, state, x, y, z); ok {
			return s
		}
	case b == registry.BlockTripwire:
		// vanilla: Block.updateFromNeighbourShapes, TripWireBlock.updateShape
		if s, ok := TripwireStateFromState(w, state, x, y, z); ok {
			return s
		}
	case b.IsCrossCollision():
		return CrossStateFromState(w, b, state, x, y, z)
	case b.IsWall():
		return WallStateFromState(w, b, state, x, y, z)
	case b.IsStairs():
		return StairsStateWithShape(w, b, state, x, y, z)
	case b.IsFenceGate():
		return FenceGateStateInWall(w, b, state, x, y, z)
	case b == registry.BlockNoteBlock:
		// A command that spelled an instrument out keeps it, which the
		// state alone shows only for one other than the default harp.
		// vanilla: BlockInput.overwriteWithDefinedProperties
		if registry.StateValue(state, registry.PropInstrument) == "harp" {
			return NoteBlockInstrumentState(w, state, x, y, z)
		}
	case b == registry.BlockFire:
		return fireNeighborShapedState(w, state, x, y, z)
	}
	shaped := world.PlacementShape(w, rng, state, x, y, z)
	// vanilla: BlockInput.place keeps the requested state when shaping returns air.
	if !world.IsKnownAirState(shaped) {
		return shaped
	}
	return state
}

// WithDefinedProperties returns state with the properties a command spelled
// out laid back over it, so a neighbour-derived shape never overrides them.
// vanilla: BlockInput.overwriteWithDefinedProperties
func WithDefinedProperties(state uint16, defined map[string]string) uint16 {
	if len(defined) == 0 {
		return state
	}
	if int(state) >= registry.StateCount {
		return state
	}
	out := state
	for key, value := range defined {
		prop, ok := registry.PropNamed(key)
		if !ok {
			return registry.BlockOf(state).DefaultState()
		}
		// A shape conversion can change block kind and remove a property.
		// vanilla: BlockStateBase.copyProperty uses trySetValue.
		if registry.StateValue(out, prop) == "" {
			continue
		}
		out, ok = registry.WithProp(out, prop, value)
		if !ok {
			return registry.BlockOf(state).DefaultState()
		}
	}
	return out
}

// ReconnectShapes recomputes the block at (x, y, z) and reshapes its four
// horizontal neighbours, for writers that bypassed the place-and-break shape
// update. It returns the writes in order.
// vanilla: Level.updateNeighbourShapes
func ReconnectShapes(w ShapeWorld, x, y, z int32) []world.BlockChange {
	var out []world.BlockChange
	out = reconnectCross(w, out, x, y, z)
	out = reconnectWall(w, out, x, y, z, allSides)
	out = reconnectStairs(w, out, x, y, z)
	out = reconnectGate(w, out, x, y, z)
	for _, ch := range out[:len(out):len(out)] {
		out = updateNeighborShapes(w, ch.X, ch.Y, ch.Z, shapeUpdateLimit-1, out)
	}
	return updateNeighborShapes(w, x, y, z, shapeUpdateLimit, out)
}

// neighborSides lists the horizontal neighbours of a changed cell: the offset
// to the neighbour and the name of the neighbour's side that faces the cell.
var neighborSides = [4]struct {
	dx, dz int32
	facing string
}{{0, -1, "south"}, {0, 1, "north"}, {1, 0, "west"}, {-1, 0, "east"}}

// shapeUpdateLimit is vanilla's Block.UPDATE_LIMIT (512) less the originating
// write.
const shapeUpdateLimit = 511

// UpdateNeighborShapes reshapes the neighbours of (x, y, z): a fence, bars or
// wall reconnects and a stair reshapes. It is the neighbour half of a
// setBlock's shape update, and returns the writes it made, in order. Each write
// runs the pass again around itself, depth first.
// vanilla: Block.updateOrDestroy, LevelAccessor.setBlock
func UpdateNeighborShapes(w ShapeWorld, x, y, z int32) []world.BlockChange {
	return updateNeighborShapes(w, x, y, z, shapeUpdateLimit, nil)
}

// updateNeighborShapes is the pass at one cell; limit is the remaining depth
// of the cascade.
func updateNeighborShapes(w ShapeWorld, x, y, z int32, limit int, out []world.BlockChange) []world.BlockChange {
	if limit <= 0 {
		return out
	}
	// Each neighbour recomputes only the side facing the changed cell, so a
	// side a command spelled out survives a change on another side.
	for _, d := range neighborSides {
		from := len(out)
		out = reconnectCrossSide(w, out, x+d.dx, y, z+d.dz, d.facing)
		out = cascadeShapes(w, out, from, limit)
	}
	// A wall also reconnects the one below, but not the one above.
	// vanilla: WallBlock.updateShape is a no-op for DOWN
	for _, d := range neighborSides {
		from := len(out)
		out = reconnectWall(w, out, x+d.dx, y, z+d.dz, d.facing)
		out = cascadeShapes(w, out, from, limit)
	}
	from := len(out)
	out = reconnectWall(w, out, x, y-1, z, "")
	out = cascadeShapes(w, out, from, limit)
	// A stair's shape reads horizontal neighbours only, so a change above or
	// below cannot reshape one.
	for _, d := range neighborSides {
		from := len(out)
		out = reconnectStairs(w, out, x+d.dx, y, z+d.dz)
		out = cascadeShapes(w, out, from, limit)
	}
	// A gate reads only the walls on its two sides, so a change above or
	// below cannot reshape one.
	for _, d := range neighborSides {
		from := len(out)
		out = reconnectGate(w, out, x+d.dx, y, z+d.dz)
		out = cascadeShapes(w, out, from, limit)
	}
	from = len(out)
	out = reconnectPlantHeads(w, out, x, y, z)
	out = cascadeShapes(w, out, from, limit)
	// A carpet reads its four sides and the carpets above and below.
	for _, d := range [6][3]int32{{0, -1, 0}, {0, 1, 0}, {1, 0, 0}, {-1, 0, 0}, {0, 0, 1}, {0, 0, -1}} {
		from := len(out)
		out = reconnectMossyCarpet(w, out, x+d[0], y+d[1], z+d[2])
		out = cascadeShapes(w, out, from, limit)
	}
	return out
}

// cascadeShapes runs the pass around every write appended since out[from].
func cascadeShapes(w ShapeWorld, out []world.BlockChange, from, limit int) []world.BlockChange {
	for i, to := from, len(out); i < to; i++ {
		ch := out[i]
		out = updateNeighborShapes(w, ch.X, ch.Y, ch.Z, limit-1, out)
	}
	return out
}

// reconnectGate recomputes in_wall of the fence gate at (x, y, z) and appends
// the write if the state changed. A non-gate is a no-op.
// vanilla: FenceGateBlock.updateShape
func reconnectGate(w ShapeWorld, out []world.BlockChange, x, y, z int32) []world.BlockChange {
	state := w.BlockAt(x, y, z)
	b := registry.BlockOf(state)
	if !b.IsFenceGate() {
		return out
	}
	newState := FenceGateStateInWall(w, b, state, x, y, z)
	if newState == state || !w.SetBlock(x, y, z, newState) {
		return out
	}
	return append(out, world.BlockChange{X: x, Y: y, Z: z, State: newState})
}

// reconnectStairs recomputes the corner shape of the stair at (x, y, z) from
// the stairs beside it and appends the write if the state changed. A non-stair
// is a no-op.
// vanilla: StairBlock.updateShape
func reconnectStairs(w ShapeWorld, out []world.BlockChange, x, y, z int32) []world.BlockChange {
	state := w.BlockAt(x, y, z)
	b := registry.BlockOf(state)
	if !b.IsStairs() {
		return out
	}
	newState := StairsStateWithShape(w, b, state, x, y, z)
	if newState == state || !w.SetBlock(x, y, z, newState) {
		return out
	}
	return append(out, world.BlockChange{X: x, Y: y, Z: z, State: newState})
}

// allSides asks reconnectWall to recompute every side, for a wall that was
// just placed.
const allSides = "all"

// reconnectWall updates a wall for a change on its facing side (or above
// when facing is empty), appending a changed state. Only that side is
// recomputed. The pass cascades the write: updateNeighborShapes runs again
// around it.
// vanilla: WallBlock.updateShape
func reconnectWall(w ShapeWorld, out []world.BlockChange, x, y, z int32, facing string) []world.BlockChange {
	state := w.BlockAt(x, y, z)
	b := registry.BlockOf(state)
	if !b.IsWall() {
		return out
	}
	newState := WallStateForNeighbor(w, b, state, x, y, z, facing)
	if newState == state || !w.SetBlock(x, y, z, newState) {
		return out
	}
	out = append(out, world.BlockChange{X: x, Y: y, Z: z, State: newState})
	return reconnectWall(w, out, x, y-1, z, "")
}

// reconnectCrossSide sets the facing side of the fence or bars at (x, y, z)
// from the cell on that side and appends the write if the state changed. A
// non-cross-collision block is a no-op.
// vanilla: FenceBlock.updateShape, IronBarsBlock.updateShape
func reconnectCrossSide(w ShapeWorld, out []world.BlockChange, x, y, z int32, facing string) []world.BlockChange {
	state := w.BlockAt(x, y, z)
	b := registry.BlockOf(state)
	if !b.IsCrossCollision() {
		return out
	}
	prop := sidePropOf(facing)
	want := CrossConnects(w, b, x, y, z, facing)
	if registry.StateBool(state, prop) == want {
		return out
	}
	newState, ok := registry.WithBool(state, prop, want)
	if !ok || newState == state || !w.SetBlock(x, y, z, newState) {
		return out
	}
	return append(out, world.BlockChange{X: x, Y: y, Z: z, State: newState})
}

// reconnectCross recomputes the connections of the fence or bars at
// (x, y, z) from its four horizontal neighbours, and appends the write if the
// state changed. A non-cross-collision block is a no-op.
// vanilla: CrossCollisionBlock.updateShape
func reconnectCross(w ShapeWorld, out []world.BlockChange, x, y, z int32) []world.BlockChange {
	state := w.BlockAt(x, y, z)
	b := registry.BlockOf(state)
	if !b.IsCrossCollision() {
		return out
	}
	newState := CrossStateFromState(w, b, state, x, y, z)
	if newState == state || !w.SetBlock(x, y, z, newState) {
		return out
	}
	return append(out, world.BlockChange{X: x, Y: y, Z: z, State: newState})
}
