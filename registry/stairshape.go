package registry

// stairDirs are the horizontal directions clockwise, from north.
var stairDirs = [4]string{"north", "east", "south", "west"}

// stairDelta is the cell offset of each stairDirs entry.
var stairDelta = [4][2]int32{{0, -1}, {1, 0}, {0, 1}, {-1, 0}}

func stairDirIndex(facing string) int {
	for i, d := range stairDirs {
		if d == facing {
			return i
		}
	}
	return 0
}

// StairsStateWithShape sets the corner shape of the stair placed at (x, y, z)
// from the stairs beside it; a block that is not a stair is unchanged.
// vanilla: StairBlock.getStairsShape
func StairsStateWithShape(w BlockReader, b Block, placed uint16, x, y, z int32) uint16 {
	if !b.IsStairs() {
		return placed
	}
	facing := stairDirIndex(StateValue(placed, PropFacing))
	shape := stairsShape(w, facing, StateValue(placed, PropHalf), x, y, z)
	if StateValue(placed, PropShape) == shape {
		return placed
	}
	placed, _ = WithProp(placed, PropShape, shape)
	return placed
}

// stairsShape: the stair the facing points at makes an outer corner, the one
// behind an inner corner, each only same-half and perpendicular, and refused
// when the corner's open side holds a stair facing this way (a straight run).
// vanilla: StairBlock.getStairsShape
func stairsShape(w BlockReader, facing int, half string, x, y, z int32) string {
	counterClockwise := (facing + 3) % 4
	if behind, ok := stairBeside(w, facing, facing, half, x, y, z); ok &&
		stairCanTakeShape(w, (behind+2)%4, facing, half, x, y, z) {
		if behind == counterClockwise {
			return "outer_left"
		}
		return "outer_right"
	}
	if front, ok := stairBeside(w, (facing+2)%4, facing, half, x, y, z); ok &&
		stairCanTakeShape(w, front, facing, half, x, y, z) {
		if front == counterClockwise {
			return "inner_left"
		}
		return "inner_right"
	}
	return "straight"
}

// stairBeside reads the cell one step along d and reports the facing of the
// stair there when it is a stair of the same half on a perpendicular axis to
// facing.
func stairBeside(w BlockReader, d, facing int, half string, x, y, z int32) (int, bool) {
	state := w.BlockAt(x+stairDelta[d][0], y, z+stairDelta[d][1])
	if !BlockOf(state).IsStairs() || StateValue(state, PropHalf) != half {
		return 0, false
	}
	neighbour := stairDirIndex(StateValue(state, PropFacing))
	if neighbour%2 == facing%2 {
		return 0, false
	}
	return neighbour, true
}

// stairCanTakeShape is StairBlock.canTakeShape: the corner is refused when
// the cell it opens toward holds a stair of the same half facing this stair's
// way.
func stairCanTakeShape(w BlockReader, d, facing int, half string, x, y, z int32) bool {
	state := w.BlockAt(x+stairDelta[d][0], y, z+stairDelta[d][1])
	if !BlockOf(state).IsStairs() {
		return true
	}
	return stairDirIndex(StateValue(state, PropFacing)) != facing || StateValue(state, PropHalf) != half
}
