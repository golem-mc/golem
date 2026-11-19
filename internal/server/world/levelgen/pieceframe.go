package levelgen

// PieceFrame is the bounding box and orientation a structure piece places its
// blocks through. Dir is 0..3 for north, east, south, west.
type PieceFrame struct {
	BB  BoundingBox
	Dir int
}

var horizontalOffsets = [4][2]int{{0, -1}, {1, 0}, {0, 1}, {-1, 0}}

// MapDir turns a piece-local direction into a world one: mirrored front to
// back when the piece faces south or west, then turned clockwise when it faces
// west or east. vanilla: StructurePiece.setOrientation
func (f PieceFrame) MapDir(d int) int {
	fd, dir := fdir(d), fdir(f.Dir)
	if (dir == dS || dir == dW) && (fd == dN || fd == dS) {
		fd = fd.opposite()
	}
	if dir == dW || dir == dE {
		fd = fd.clockwise()
	}
	return int(fd)
}

// WorldPos is vanilla: StructurePiece.getWorldX, getWorldY, getWorldZ.
func (f PieceFrame) WorldPos(x, y, z int) (int, int, int) {
	var wx, wz int
	switch fdir(f.Dir) {
	case dN:
		wx, wz = f.BB.MinX+x, f.BB.MaxZ-z
	case dS:
		wx, wz = f.BB.MinX+x, f.BB.MinZ+z
	case dW:
		wx, wz = f.BB.MaxX-z, f.BB.MinZ+x
	default:
		wx, wz = f.BB.MinX+z, f.BB.MinZ+x
	}
	return wx, y + f.BB.MinY, wz
}

// GenerateBox visits every block of a piece-local box, saying whether it is on
// the shell. vanilla: StructurePiece.generateBox
func GenerateBox(x0, y0, z0, x1, y1, z1 int, visit func(x, y, z int, shell bool)) {
	for y := y0; y <= y1; y++ {
		for x := x0; x <= x1; x++ {
			for z := z0; z <= z1; z++ {
				visit(x, y, z, y == y0 || y == y1 || x == x0 || x == x1 || z == z0 || z == z1)
			}
		}
	}
}

// ChestWorld is the level a piece's chest is placed into.
type ChestWorld interface {
	Block(x, y, z int) uint16
	SolidRender(state uint16) bool
	IsChest(state uint16) bool
	// PlaceChest sets a chest facing north, east, south or west (0..3) and
	// records its loot table, drawing the loot seed from the placement random.
	PlaceChest(x, y, z int, facing int)
}

// CreateChest places a chest at a piece-local block inside chunk unless one is
// already there, and reports whether the block is inside chunk.
// vanilla: StructurePiece.createChest
func (f PieceFrame) CreateChest(w ChestWorld, chunk BoundingBox, x, y, z int) bool {
	wx, wy, wz := f.WorldPos(x, y, z)
	if !chunk.inside(wx, wy, wz) {
		return false
	}
	if !w.IsChest(w.Block(wx, wy, wz)) {
		w.PlaceChest(wx, wy, wz, reorientChest(w, wx, wy, wz))
	}
	return true
}

// reorientChest returns the facing of a chest placed at a block: away from
// its one solid neighbour, else the first open side. vanilla: StructurePiece.reorient
func reorientChest(w ChestWorld, x, y, z int) int {
	solid := func(d int) bool {
		o := horizontalOffsets[d]
		return w.SolidRender(w.Block(x+o[0], y, z+o[1]))
	}
	wall := -1
	for d := range horizontalOffsets {
		o := horizontalOffsets[d]
		if w.IsChest(w.Block(x+o[0], y, z+o[1])) {
			return int(dN)
		}
		if !solid(d) {
			continue
		}
		if wall < 0 {
			wall = d
			continue
		}
		wall = -1
		break
	}
	if wall >= 0 {
		return int(fdir(wall).opposite())
	}
	lock := dN
	if solid(int(lock)) {
		lock = lock.opposite()
	}
	if solid(int(lock)) {
		lock = lock.clockwise()
	}
	if solid(int(lock)) {
		lock = lock.opposite()
	}
	return int(lock)
}
