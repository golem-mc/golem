package rules

import (
	"math"

	"github.com/golem-mc/golem/registry"
)

// End portal completion: which nine cells fill when an eye lands in a full
// twelve-frame ring. Writing the blocks and the sounds is internal/server/portal.go's.

// The ring the completion walks, five by five: twelve frames that need an
// eye around the edge, "anywhere" in the middle and corners. 'v' faces
// north, '^' south, '<' east, '>' west, so every eye points at the middle.
// vanilla: EndPortalFrameBlock.getOrCreatePortalShape
var endPortalShape = [5]string{
	"?vvv?",
	">???<",
	">???<",
	">???<",
	"?^^^?",
}

// The facing each of the four pattern cells demands of its frame.
var endPortalShapeFacings = map[byte]string{
	'v': "north",
	'^': "south",
	'<': "east",
	'>': "west",
}

// The six directions of the pairs the search squares the pattern onto, in
// the order the search tries them: down, up, north, south, west, east.
var endPortalDirections = [][3]int32{
	{0, -1, 0},
	{0, 1, 0},
	{0, 0, -1},
	{0, 0, 1},
	{-1, 0, 0},
	{1, 0, 0},
}

// EndPortalInterior returns the nine cells an eye placed at (x, y, z) fills
// and whether the ring is complete. It tries each origin in the 5x5x5 the
// eye is the low corner of, then each direction pair; the first match's low
// corner stands for the middle. Cells come in fill order, X slowest, Z
// fastest.
// vanilla: BlockPattern.find, EnderEyeItem.useOn
func EndPortalInterior(w BlockReader, x, y, z int32) ([][3]int32, bool) {
	for oz := z; oz <= z+4; oz++ {
		for oy := y; oy <= y+4; oy++ {
			for ox := x; ox <= x+4; ox++ {
				for _, forwards := range endPortalDirections {
					for _, upwards := range endPortalDirections {
						// A pair of parallels squares the pattern onto
						// nothing; translateAndRotate refuses them.
						// vanilla: BlockPattern.translateAndRotate
						if forwards == upwards || forwards == [3]int32{-upwards[0], -upwards[1], -upwards[2]} {
							continue
						}
						if !endPortalShapeMatches(w, ox, oy, oz, forwards, upwards) {
							continue
						}
						interior := make([][3]int32, 9)
						for i := range interior {
							interior[i] = [3]int32{ox - 3 + int32(i/3), oy, oz - 3 + int32(i%3)}
						}
						return interior, true
					}
				}
			}
		}
	}
	return nil, false
}

// endPortalShapeMatches reports whether the pattern fits at (ox, oy, oz):
// columns along forwards × upwards, rows back along upwards, every frame cell
// a frame with its eye, facing as the pattern reads.
func endPortalShapeMatches(w BlockReader, ox, oy, oz int32, forwards, upwards [3]int32) bool {
	cross := [3]int32{
		forwards[1]*upwards[2] - forwards[2]*upwards[1],
		forwards[2]*upwards[0] - forwards[0]*upwards[2],
		forwards[0]*upwards[1] - forwards[1]*upwards[0],
	}
	for row := range int32(5) {
		for col := range int32(5) {
			want, anywhere := endPortalShapeFacings[endPortalShape[row][col]]
			if !anywhere {
				continue
			}
			state := w.BlockAt(
				ox+cross[0]*col-upwards[0]*row,
				oy+cross[1]*col-upwards[1]*row,
				oz+cross[2]*col-upwards[2]*row)
			if registry.BlockOf(state) != registry.BlockEndPortalFrame {
				return false
			}
			if !registry.StateBool(state, registry.PropEye) || registry.StateValue(state, registry.PropFacing) != want {
				return false
			}
		}
	}
	return true
}

// GlobalEventAt is where a listener at (px, py, pz) in the source's
// dimension hears a global world event from (cx, cy, cz): within 32 blocks,
// at the source; farther, 32 blocks along the line to it.
// vanilla: ServerLevel.globalLevelEvent
func GlobalEventAt(px, py, pz, cx, cy, cz float64) (x, y, z float64) {
	dx, dy, dz := cx-px, cy-py, cz-pz
	if dx*dx+dy*dy+dz*dz < 32*32 {
		return cx, cy, cz
	}
	f := 32 / math.Sqrt(dx*dx+dy*dy+dz*dz)
	return px + dx*f, py + dy*f, pz + dz*f
}
