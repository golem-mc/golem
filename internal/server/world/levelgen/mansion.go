package levelgen

// Mansion room cells pack a room id, its size and placement flags.
// vanilla: WoodlandMansionPieces.MansionGrid
const (
	MansionRoom1x1      = 1 << 16
	MansionRoom1x2      = 2 << 16
	MansionRoom2x2      = 4 << 16
	MansionRoomOrigin   = 1 << 20
	MansionRoomDoor     = 1 << 21
	MansionRoomStairs   = 1 << 22
	MansionRoomCorridor = 1 << 23
	MansionRoomTypeMask = 0xf0000
	MansionRoomIDMask   = 0xffff
)

// MansionFloor is an 11 by 11 grid indexed by x then z. Footprints use 0 for
// clear, 1 for corridor, 2 for room, 3 for start room and 5 for blocked.
// vanilla: WoodlandMansionPieces.SimpleGrid
type MansionFloor [11][11]int

func (g *MansionFloor) get(x, z int) int {
	if x < 0 || x >= 11 || z < 0 || z >= 11 {
		return 5
	}
	return g[x][z]
}

func (g *MansionFloor) set(x, z, v int) {
	if x >= 0 && x < 11 && z >= 0 && z < 11 {
		g[x][z] = v
	}
}

func (g *MansionFloor) fill(x0, z0, x1, z1, v int) {
	for z := z0; z <= z1; z++ {
		for x := x0; x <= x1; x++ {
			g.set(x, z, v)
		}
	}
}

func (g *MansionFloor) setClear(x, z, v int) {
	if g.get(x, z) == 0 {
		g.set(x, z, v)
	}
}

// IsHouse reports whether the footprint cell belongs to the mansion.
func (g *MansionFloor) IsHouse(x, z int) bool {
	v := g.get(x, z)
	return v >= 1 && v <= 4
}

func (g *MansionFloor) edgesToCorridor(x, z int) bool {
	return g.get(x-1, z) == 1 || g.get(x+1, z) == 1 || g.get(x, z+1) == 1 || g.get(x, z-1) == 1
}

// MansionGrid holds the shared lower-floor footprint, the third-floor
// footprint and the room ids and flags for each floor.
// vanilla: WoodlandMansionPieces.MansionGrid
type MansionGrid struct {
	Base, Third MansionFloor
	Rooms       [3]MansionFloor
}

type mansionGridBuilder struct {
	*MansionGrid
	rng RandomSource
}

// NewMansionGrid grows the corridors, groups rooms and chooses the staircase;
// rng continues at the draw used next by the template placer.
func NewMansionGrid(rng RandomSource) *MansionGrid {
	g := &mansionGridBuilder{MansionGrid: &MansionGrid{}, rng: rng}
	b := &g.Base
	b.fill(7, 4, 8, 5, 3)
	b.fill(6, 4, 6, 5, 2)
	b.fill(9, 2, 10, 7, 5)
	b.fill(8, 2, 8, 3, 1)
	b.fill(8, 6, 8, 7, 1)
	b.set(6, 3, 1)
	b.set(6, 6, 1)
	b.fill(0, 0, 11, 1, 5)
	b.fill(0, 9, 11, 11, 5)
	g.corridor(b, 7, 2, dW, 6)
	g.corridor(b, 7, 7, dW, 6)
	g.corridor(b, 5, 3, dW, 3)
	g.corridor(b, 5, 6, dW, 3)
	for mansionCleanEdges(b) {
	}
	g.identifyRooms(b, &g.Rooms[0])
	g.identifyRooms(b, &g.Rooms[1])
	g.Rooms[0].fill(8, 4, 8, 5, MansionRoomCorridor)
	g.Rooms[1].fill(8, 4, 8, 5, MansionRoomCorridor)
	g.setupThirdFloor()
	g.identifyRooms(&g.Third, &g.Rooms[2])
	return g.MansionGrid
}

// Direction.Plane.HORIZONTAL iterates north, east, south, west; the random
// from2DDataValue order is south, west, north, east.
var mansionDX = [4]int{0, 1, 0, -1}
var mansionDZ = [4]int{-1, 0, 1, 0}

// vanilla: WoodlandMansionPieces.MansionGrid.recursiveCorridor
func (g *mansionGridBuilder) corridor(b *MansionFloor, x, z int, heading fdir, depth int) {
	if depth <= 0 {
		return
	}
	dx, dz := mansionDX[heading], mansionDZ[heading]
	b.set(x, z, 1)
	b.setClear(x+dx, z+dz, 1)
	for range 8 {
		d := fdir((g.rng.NextIntN(4) + 2) & 3)
		if d == heading.opposite() || d == dE && g.rng.NextBoolean() {
			continue
		}
		nx, nz := x+dx, z+dz
		if b.get(nx+mansionDX[d], nz+mansionDZ[d]) != 0 || b.get(nx+2*mansionDX[d], nz+2*mansionDZ[d]) != 0 {
			continue
		}
		g.corridor(b, nx+mansionDX[d], nz+mansionDZ[d], d, depth-1)
		break
	}
	cw, ccw := heading.clockwise(), heading.counterClockwise()
	for _, p := range [][2]int{
		{x + mansionDX[cw], z + mansionDZ[cw]}, {x + mansionDX[ccw], z + mansionDZ[ccw]},
		{x + dx + mansionDX[cw], z + dz + mansionDZ[cw]}, {x + dx + mansionDX[ccw], z + dz + mansionDZ[ccw]},
		{x + 2*dx, z + 2*dz}, {x + 2*mansionDX[cw], z + 2*mansionDZ[cw]}, {x + 2*mansionDX[ccw], z + 2*mansionDZ[ccw]},
	} {
		b.setClear(p[0], p[1], 2)
	}
}

// vanilla: WoodlandMansionPieces.MansionGrid.cleanEdges
func mansionCleanEdges(b *MansionFloor) bool {
	touched := false
	for z := range 11 {
		for x := range 11 {
			if b.get(x, z) != 0 {
				continue
			}
			direct, diagonal := 0, 0
			for d := range 4 {
				if b.IsHouse(x+mansionDX[d], z+mansionDZ[d]) {
					direct++
				}
			}
			if direct == 2 {
				for _, p := range [][2]int{{1, 1}, {-1, 1}, {1, -1}, {-1, -1}} {
					if b.IsHouse(x+p[0], z+p[1]) {
						diagonal++
					}
				}
			}
			if direct >= 3 || direct == 2 && diagonal <= 1 {
				b.set(x, z, 2)
				touched = true
			}
		}
	}
	return touched
}

// vanilla: WoodlandMansionPieces.MansionGrid.setupThirdFloor
func (g *mansionGridBuilder) setupThirdFloor() {
	var potential [][2]int
	floor := &g.Rooms[1]
	for z := range 11 {
		for x := range 11 {
			v := floor.get(x, z)
			if v&MansionRoomTypeMask == MansionRoom1x2 && v&MansionRoomDoor != 0 {
				potential = append(potential, [2]int{x, z})
			}
		}
	}
	if len(potential) == 0 {
		g.Third.fill(0, 0, 11, 11, 5)
		return
	}
	p := potential[g.rng.NextIntN(int32(len(potential)))]
	x, z := p[0], p[1]
	v := floor.get(x, z)
	floor.set(x, z, v|MansionRoomStairs)
	ex, ez := x, z
	for d := range 4 {
		if floor.get(x+mansionDX[d], z+mansionDZ[d])&MansionRoomIDMask == v&MansionRoomIDMask {
			ex, ez = x+mansionDX[d], z+mansionDZ[d]
			break
		}
	}
	for rz := range 11 {
		for rx := range 11 {
			switch {
			case !g.Base.IsHouse(rx, rz):
				g.Third.set(rx, rz, 5)
			case rx == x && rz == z:
				g.Third.set(rx, rz, 3)
			case rx == ex && rz == ez:
				g.Third.set(rx, rz, 3)
				g.Rooms[2].set(rx, rz, MansionRoomCorridor)
			}
		}
	}
	var dirs []fdir
	for d := dN; d <= dW; d++ {
		if g.Third.get(ex+mansionDX[d], ez+mansionDZ[d]) == 0 {
			dirs = append(dirs, d)
		}
	}
	if len(dirs) == 0 {
		g.Third.fill(0, 0, 11, 11, 5)
		floor.set(x, z, v)
		return
	}
	d := dirs[g.rng.NextIntN(int32(len(dirs)))]
	g.corridor(&g.Third, ex+mansionDX[d], ez+mansionDZ[d], d, 4)
	for mansionCleanEdges(&g.Third) {
	}
}

// vanilla: WoodlandMansionPieces.MansionGrid.identifyRooms
func (g *mansionGridBuilder) identifyRooms(from, rooms *MansionFloor) {
	var positions [][2]int
	for z := range 11 {
		for x := range 11 {
			if from.get(x, z) == 2 {
				positions = append(positions, [2]int{x, z})
			}
		}
	}
	// vanilla: Util.shuffle (backwards Fisher-Yates)
	for i := len(positions); i > 1; i-- {
		j := int(g.rng.NextIntN(int32(i)))
		positions[i-1], positions[j] = positions[j], positions[i-1]
	}
	id := 10
	for _, p := range positions {
		x, z := p[0], p[1]
		if rooms.get(x, z) != 0 {
			continue
		}
		x0, x1, z0, z1, typ := x, x, z, z, MansionRoom1x1
		free := func(dx, dz int) bool { return rooms.get(x+dx, z+dz) == 0 && from.get(x+dx, z+dz) == 2 }
		switch {
		case free(1, 0) && free(0, 1) && free(1, 1):
			x1++
			z1++
			typ = MansionRoom2x2
		case free(-1, 0) && free(0, 1) && free(-1, 1):
			x0--
			z1++
			typ = MansionRoom2x2
		case free(-1, 0) && free(0, -1) && free(-1, -1):
			x0--
			z0--
			typ = MansionRoom2x2
		case free(1, 0):
			x1++
			typ = MansionRoom1x2
		case free(0, 1):
			z1++
			typ = MansionRoom1x2
		case free(-1, 0):
			x0--
			typ = MansionRoom1x2
		case free(0, -1):
			z0--
			typ = MansionRoom1x2
		}
		doorX, doorZ := x1, z1
		if g.rng.NextBoolean() {
			doorX = x0
		}
		if g.rng.NextBoolean() {
			doorZ = z0
		}
		flip := func(v, lo, hi int) int {
			if v == lo {
				return hi
			}
			return lo
		}
		doorFlag := MansionRoomDoor
		if !from.edgesToCorridor(doorX, doorZ) {
			doorX, doorZ = flip(doorX, x0, x1), flip(doorZ, z0, z1)
			if !from.edgesToCorridor(doorX, doorZ) {
				doorZ = flip(doorZ, z0, z1)
				if !from.edgesToCorridor(doorX, doorZ) {
					doorX, doorZ = flip(doorX, x0, x1), flip(doorZ, z0, z1)
					if !from.edgesToCorridor(doorX, doorZ) {
						doorFlag, doorX, doorZ = 0, x0, z0
					}
				}
			}
		}
		rooms.fill(x0, z0, x1, z1, typ|id)
		rooms.set(doorX, doorZ, MansionRoomOrigin|doorFlag|typ|id)
		id++
	}
}
