package rules

// The item frame's geometry on the wall it was placed against. Who hears
// about it is the server's; this is the box vanilla's ItemFrame builds and
// the block cells that carry it.

// ItemFrameDepth is the sliver a frame occupies along its facing, the
// 1/16th of a block vanilla's DEPTH constant.
const ItemFrameDepth = 0.0625

// ItemFrameWidth is the frame's side, the 3/4 of a block vanilla's WIDTH
// and HEIGHT constants — the border drawn within the cell's face.
const ItemFrameWidth = 0.75

// ItemFrameMapWidth is the side of a frame holding a map: the whole face,
// the map being drawn over the border.
const ItemFrameMapWidth = 1.0

// ItemFrameBox is vanilla ItemFrame.createBoundingBox: the frame's sliver
// in the cell it hangs in, against the block behind it. x, y, z is the
// hanging cell — the cell the clicked face points into — and dir the
// direction it faces, the way the packets number them. framedMap widens it
// to the whole face; the pop box (getPopBox) never is.
func ItemFrameBox(x, y, z, dir int32, framedMap bool) (minX, minY, minZ, maxX, maxY, maxZ float64) {
	// The box centres on the cell's own centre, held a thirty-second of a
	// block off the wall's surface (vanilla's relative(direction, -0.46875)).
	step := dirDeltas[int(dir)]
	cx := float64(x) + 0.5 - float64(step[0])*0.46875
	cy := float64(y) + 0.5 - float64(step[1])*0.46875
	cz := float64(z) + 0.5 - float64(step[2])*0.46875
	width := ItemFrameWidth
	if framedMap {
		width = ItemFrameMapWidth
	}
	xSize, ySize, zSize := width, width, width
	switch {
	case step[0] != 0:
		xSize = ItemFrameDepth
	case step[1] != 0:
		ySize = ItemFrameDepth
	default:
		zSize = ItemFrameDepth
	}
	return cx - xSize/2, cy - ySize/2, cz - zSize/2,
		cx + xSize/2, cy + ySize/2, cz + zSize/2
}

// ItemFrameSupportCell is the one block cell a frame's support is: the
// cell behind the one it hangs in, the block it was placed on.
func ItemFrameSupportCell(x, y, z, dir int32) [3]int32 {
	step := dirDeltas[int(dir)]
	return [3]int32{x - step[0], y - step[1], z - step[2]}
}
