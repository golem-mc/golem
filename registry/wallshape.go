package registry

import "strconv"

// BlockReader is the slice of the world the shape rules read: block lookup
// by world coordinates.
type BlockReader interface {
	BlockAt(x, y, z int32) uint16
}

// HorizontalSideProps are the north, south, east and west connection
// properties, in that order.
var HorizontalSideProps = [4]Prop{PropNorth, PropSouth, PropEast, PropWest}

// SideOffset returns the horizontal offset of the neighbour on a side;
// anything but north, south or east reads as west.
func SideOffset(dir string) (dx, dz int32) {
	switch dir {
	case "north":
		return 0, -1
	case "south":
		return 0, 1
	case "east":
		return 1, 0
	}
	return -1, 0
}

// wallSide is a wall's side property: none, low or tall. A connected arm is
// tall when the block above's down-face collision covers the arm's
// footprint; air or a top slab keeps it low.
type wallSide string

const (
	wallNone wallSide = "none"
	wallLow  wallSide = "low"
	wallTall wallSide = "tall"
)

// WallConnections reports which horizontal neighbours a wall connects to:
// walls, iron bars, fence gates from the side, sturdy faces.
// vanilla: WallBlock.connectsTo
func WallConnections(w BlockReader, x, y, z int32) (north, south, east, west bool) {
	north = ConnectsToWall(w.BlockAt(x, y, z-1), "north")
	south = ConnectsToWall(w.BlockAt(x, y, z+1), "south")
	east = ConnectsToWall(w.BlockAt(x+1, y, z), "east")
	west = ConnectsToWall(w.BlockAt(x-1, y, z), "west")
	return north, south, east, west
}

// WallStateWithConnections is the wall's placed state from its neighbours
// and the block above: sides none/low/tall by connection and cover, up by the
// corner shape and the block above.
// vanilla: WallBlock.getStateForPlacement, updateShape
func WallStateWithConnections(w BlockReader, b Block, x, y, z int32) uint16 {
	return WallStateFromState(w, b, b.DefaultState(), x, y, z)
}

// WallStateFromState returns the state of wall b at (x, y, z)
// with its sides and post recomputed from the neighbours; base's other
// properties, waterlogged among them, are kept.
func WallStateFromState(w BlockReader, b Block, base uint16, x, y, z int32) uint16 {
	north, south, east, west := WallConnections(w, x, y, z)
	above := w.BlockAt(x, y+1, z)
	return wallStateFrom(b, base, north, south, east, west, above)
}

// WallStateForNeighbor returns the wall state after the cell on the facing
// side changed (the cell above when facing is empty; "all" for every side):
// only that side is recomputed; above and the post follow.
// vanilla: WallBlock.updateShape
func WallStateForNeighbor(w BlockReader, b Block, base uint16, x, y, z int32, facing string) uint16 {
	var conn [4]bool
	for i, d := range [4]string{"north", "south", "east", "west"} {
		if d == facing || facing == "all" {
			dx, dz := SideOffset(d)
			conn[i] = ConnectsToWall(w.BlockAt(x+dx, y, z+dz), d)
		} else {
			conn[i] = StateValue(base, HorizontalSideProps[i]) != "none"
		}
	}
	above := w.BlockAt(x, y+1, z)
	return wallStateFrom(b, base, conn[0], conn[1], conn[2], conn[3], above)
}

// WallStateFromConnections builds the wall state from the four connections
// and the block above, which makes arms tall or low and decides the post.
func WallStateFromConnections(b Block, north, south, east, west bool, above uint16) uint16 {
	return wallStateFrom(b, b.DefaultState(), north, south, east, west, above)
}

func wallStateFrom(b Block, base uint16, north, south, east, west bool, above uint16) uint16 {
	aboveOK := int(above) < StateCount
	aboveBlock := BlockOf(above)
	aboveCov := computeWallAboveCoverage(above, aboveOK)

	nSide := wallSideFromConnection(north, aboveCov.north)
	sSide := wallSideFromConnection(south, aboveCov.south)
	eSide := wallSideFromConnection(east, aboveCov.east)
	wSide := wallSideFromConnection(west, aboveCov.west)

	// A wall post above, or any block in #wall_post_override (a torch, a
	// sign, ...), forces the post regardless of the sides.
	abovePostForcing := aboveCov.wallPost || aboveOK && aboveBlock.IsWallPostOverride()
	up := shouldRaisePost(nSide, sSide, eSide, wSide, abovePostForcing, aboveCov.post)

	if BlockOf(base) == b && StateBool(base, PropUp) == up &&
		StateValue(base, PropNorth) == string(nSide) &&
		StateValue(base, PropSouth) == string(sSide) &&
		StateValue(base, PropEast) == string(eSide) &&
		StateValue(base, PropWest) == string(wSide) {
		return base
	}
	if s, ok := WithProps(base,
		PropValue{PropNorth, string(nSide)},
		PropValue{PropSouth, string(sSide)},
		PropValue{PropEast, string(eSide)},
		PropValue{PropWest, string(wSide)},
		PropValue{PropUp, strconv.FormatBool(up)},
	); ok {
		return s
	}
	return b.DefaultState()
}

// wallAboveCoverage reports per direction whether the block above covers the
// arm test shape (tall arm), and whether it covers the post test (raised
// post). A wall above covers an arm with its side there, and the post with
// up=true or any arm (arms are wider than the post test). Any other block
// goes by its down-face collision: a bottom slab or full cube covers all; a
// top slab, air or a torch nothing.
type wallAboveCoverage struct {
	north, south, east, west, post bool
	wallPost                       bool
}

func computeWallAboveCoverage(above uint16, aboveOK bool) wallAboveCoverage {
	if !aboveOK {
		return wallAboveCoverage{}
	}
	if BlockOf(above).IsWall() {
		side := func(prop Prop) bool {
			v := StateValue(above, prop)
			return v != "none" && v != ""
		}
		n := side(PropNorth)
		s := side(PropSouth)
		e := side(PropEast)
		w := side(PropWest)
		up := StateBool(above, PropUp)
		return wallAboveCoverage{
			north: n, south: s, east: e, west: w,
			post:     up || n || s || e || w,
			wallPost: up,
		}
	}
	post, n, s, w, e := WallCover(above)
	return wallAboveCoverage{north: n, south: s, east: e, west: w, post: post}
}

// wallSideFromConnection returns the wall side property for a connection: tall
// if connected and the block above covers the arm, low if connected but not
// covered, none if not connected.
func wallSideFromConnection(connected, aboveFull bool) wallSide {
	if !connected {
		return wallNone
	}
	if aboveFull {
		return wallTall
	}
	return wallLow
}

// shouldRaisePost reports whether up is true: the block above forces a post
// (a wall post, or #wall_post_override), there is a corner (all sides none,
// north≠south, or east≠west), or aboveFull. aboveFull only decides the
// uncovered cases: a straight run with both arms tall is caught by
// hasHighWall first, so a full cube over it stays flat.
// vanilla: WallBlock.shouldRaisePost
func shouldRaisePost(north, south, east, west wallSide, abovePostForcing, aboveFull bool) bool {
	if abovePostForcing {
		return true
	}
	northNone := north == wallNone
	southNone := south == wallNone
	eastNone := east == wallNone
	westNone := west == wallNone
	hasCorner := northNone && southNone && eastNone && westNone ||
		northNone != southNone || eastNone != westNone
	if hasCorner {
		return true
	}
	hasHighWall := north == wallTall && south == wallTall ||
		east == wallTall && west == wallTall
	if hasHighWall {
		return false
	}
	return aboveFull
}
