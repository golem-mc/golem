package levelgen

// MonumentRoom is one node of the monument's room graph. Connections and
// Openings use vanilla's 3D direction order: down, up, north, south, west, east.
// vanilla: OceanMonumentPieces.RoomDefinition
type MonumentRoom struct {
	Index       int
	Connections [6]*MonumentRoom
	Openings    [6]bool
	claimed     bool
	source      bool
	scanIndex   int
}

// MonumentDown through MonumentEast are vanilla's 3D direction indices.
const (
	MonumentDown = iota
	MonumentUp
	MonumentNorth
	MonumentSouth
	MonumentWest
	MonumentEast
)

func (r *MonumentRoom) connect(d int, other *MonumentRoom) {
	r.Connections[d], other.Connections[d^1] = other, r
}

func (r *MonumentRoom) updateOpenings() {
	for d, other := range r.Connections {
		r.Openings[d] = other != nil
	}
}

func (r *MonumentRoom) findSource(scan int) bool {
	if r.source {
		return true
	}
	r.scanIndex = scan
	for d, other := range r.Connections {
		if other != nil && r.Openings[d] && other.scanIndex != scan && other.findSource(scan) {
			return true
		}
	}
	return false
}

// MonumentPieceKind identifies the monument's block placers.
type MonumentPieceKind uint8

const (
	MonumentEntry MonumentPieceKind = iota
	MonumentCore
	MonumentDoubleXY
	MonumentDoubleYZ
	MonumentDoubleZ
	MonumentDoubleX
	MonumentDoubleY
	MonumentSimpleTop
	MonumentSimple
	MonumentWing
	MonumentPenthouse
)

// MonumentPiece is a fitted room or exterior piece; Design selects a simple
// room's decoration or a wing's elder-guardian chamber.
type MonumentPiece struct {
	Kind   MonumentPieceKind
	Room   *MonumentRoom
	BB     BoundingBox
	Design int
}

// MonumentLayout holds the ordered room graph and fitted pieces of a building.
// Dir is 0..3 for north, east, south, west, as in PieceFrame.
type MonumentLayout struct {
	BB           BoundingBox
	Dir          int
	Rooms        []*MonumentRoom
	Source, Core *MonumentRoom
	Pieces       []MonumentPiece
}

// GenerateMonument assembles a building at its northwest corner. The caller
// draws the orientation before passing the structure start's random stream.
// vanilla: OceanMonumentPieces.MonumentBuilding
func GenerateMonument(rng RandomSource, west, north, dir int) *MonumentLayout {
	m := &MonumentLayout{BB: BoundingBox{west, 39, north, west + 57, 61, north + 57}, Dir: dir}
	m.generateGraph(rng)
	m.Source.claimed = true
	m.addRoom(MonumentEntry, m.Source, 1, 1, 1)
	m.addRoom(MonumentCore, m.Core, 2, 2, 2)
	for _, r := range m.Rooms {
		if r.claimed || r.Index >= 75 {
			continue
		}
		m.fitRoom(rng, r)
	}
	frame := PieceFrame{BB: m.BB, Dir: dir}
	x, y, z := frame.WorldPos(9, 0, 22)
	for i := range m.Pieces {
		b := &m.Pieces[i].BB
		b.MinX, b.MaxX = b.MinX+x, b.MaxX+x
		b.MinY, b.MaxY = b.MinY+y, b.MaxY+y
		b.MinZ, b.MaxZ = b.MinZ+z, b.MaxZ+z
	}
	wing := int(rng.NextInt())
	for i, local := range [][6]int{{1, 1, 1, 23, 8, 21}, {34, 1, 1, 56, 8, 21}, {22, 13, 22, 35, 17, 35}} {
		x0, y0, z0 := frame.WorldPos(local[0], local[1], local[2])
		x1, y1, z1 := frame.WorldPos(local[3], local[4], local[5])
		p := MonumentPiece{Kind: MonumentWing, Design: (wing + i) & 1,
			BB: BoundingBox{min(x0, x1), min(y0, y1), min(z0, z1), max(x0, x1), max(y0, y1), max(z0, z1)}}
		if i == 2 {
			p.Kind, p.Design = MonumentPenthouse, 0
		}
		m.Pieces = append(m.Pieces, p)
	}
	return m
}

// vanilla: MonumentBuilding.generateRoomGraph
func (m *MonumentLayout) generateGraph(rng RandomSource) {
	var grid [75]*MonumentRoom
	index := func(x, y, z int) int { return y*25 + z*5 + x }
	for y := range 3 {
		for x := range 5 {
			for z := range 4 {
				if y == 2 && (x == 0 || x == 4 || z >= 2) {
					continue
				}
				i := index(x, y, z)
				grid[i] = &MonumentRoom{Index: i}
			}
		}
	}
	m.Source = grid[2]
	steps := [6][3]int{{0, -1, 0}, {0, 1, 0}, {0, 0, -1}, {0, 0, 1}, {-1, 0, 0}, {1, 0, 0}}
	for x := range 5 {
		for z := range 5 {
			for y := range 3 {
				r := grid[index(x, y, z)]
				if r == nil {
					continue
				}
				for d, step := range steps {
					nx, ny, nz := x+step[0], y+step[1], z+step[2]
					if nx < 0 || nx >= 5 || ny < 0 || ny >= 3 || nz < 0 || nz >= 5 {
						continue
					}
					if other := grid[index(nx, ny, nz)]; other != nil {
						// Grid z runs opposite to the piece-local north/south labels.
						connection := d
						if nz != z {
							connection ^= 1
						}
						r.connect(connection, other)
					}
				}
			}
		}
	}
	roof, left, right := &MonumentRoom{Index: 1003, claimed: true}, &MonumentRoom{Index: 1001, claimed: true}, &MonumentRoom{Index: 1002, claimed: true}
	grid[52].connect(MonumentUp, roof)
	grid[25].connect(MonumentSouth, left)
	grid[29].connect(MonumentSouth, right)
	m.Source.source = true
	m.Core = grid[index(int(rng.NextIntN(4)), 0, 2)]
	for _, r := range []*MonumentRoom{m.Core, m.Core.Connections[MonumentEast], m.Core.Connections[MonumentNorth], m.Core.Connections[MonumentEast].Connections[MonumentNorth]} {
		r.claimed, r.Connections[MonumentUp].claimed = true, true
	}
	for _, r := range grid {
		if r != nil {
			r.updateOpenings()
			m.Rooms = append(m.Rooms, r)
		}
	}
	// Only the roof's reverse connection is opened in vanilla.
	roof.updateOpenings()
	for i := len(m.Rooms); i > 1; i-- {
		j := int(rng.NextIntN(int32(i)))
		m.Rooms[i-1], m.Rooms[j] = m.Rooms[j], m.Rooms[i-1]
	}
	scan := 1
	for _, r := range m.Rooms {
		closed := 0
		for attempts := 0; closed < 2 && attempts < 5; attempts++ {
			d := int(rng.NextIntN(6))
			if !r.Openings[d] {
				continue
			}
			other := r.Connections[d]
			r.Openings[d], other.Openings[d^1] = false, false
			first := r.findSource(scan)
			scan++
			second := false
			// Short-circuiting also controls the scan counter in Java.
			if first {
				second = other.findSource(scan)
				scan++
			}
			if first && second {
				closed++
			} else {
				r.Openings[d], other.Openings[d^1] = true, true
			}
		}
	}
	m.Rooms = append(m.Rooms, roof, left, right)
}

// vanilla: OceanMonumentPiece.makeBoundingBox
func (m *MonumentLayout) addRoom(kind MonumentPieceKind, r *MonumentRoom, w, h, d int) {
	x, y, z := r.Index%5, r.Index/25, r.Index/5%5
	width, depth := w*8, d*8
	if m.Dir == int(dW) || m.Dir == int(dE) {
		width, depth = depth, width
	}
	var ox, oz int
	switch fdir(m.Dir) {
	case dN:
		ox, oz = x*8, -(z+d)*8+1
	case dS:
		ox, oz = x*8, z*8
	case dW:
		ox, oz = -(z+d)*8+1, x*8
	case dE:
		ox, oz = z*8, x*8
	}
	m.Pieces = append(m.Pieces, MonumentPiece{Kind: kind, Room: r,
		BB: BoundingBox{ox, y * 4, oz, ox + width - 1, y*4 + h*4 - 1, oz + depth - 1}})
}

// vanilla: OceanMonumentPieces.Fit*Room, in MonumentBuilding's priority order.
func (m *MonumentLayout) fitRoom(rng RandomSource, r *MonumentRoom) {
	available := func(r *MonumentRoom, d int) bool {
		return r.Openings[d] && !r.Connections[d].claimed
	}
	for _, fit := range []struct {
		kind MonumentPieceKind
		dir  int
	}{{MonumentDoubleXY, MonumentEast}, {MonumentDoubleYZ, MonumentNorth}} {
		if !available(r, fit.dir) || !available(r, MonumentUp) {
			continue
		}
		other := r.Connections[fit.dir]
		if !available(other, MonumentUp) {
			continue
		}
		r.claimed, other.claimed = true, true
		r.Connections[MonumentUp].claimed, other.Connections[MonumentUp].claimed = true, true
		w, d := 2, 1
		if fit.kind == MonumentDoubleYZ {
			w, d = 1, 2
		}
		m.addRoom(fit.kind, r, w, 2, d)
		return
	}
	for _, fit := range []struct {
		kind    MonumentPieceKind
		dir     int
		w, h, d int
	}{{MonumentDoubleZ, MonumentNorth, 1, 1, 2}, {MonumentDoubleX, MonumentEast, 2, 1, 1}, {MonumentDoubleY, MonumentUp, 1, 2, 1}} {
		if available(r, fit.dir) {
			r.claimed, r.Connections[fit.dir].claimed = true, true
			m.addRoom(fit.kind, r, fit.w, fit.h, fit.d)
			return
		}
	}
	kind := MonumentSimpleTop
	for _, d := range []int{MonumentWest, MonumentEast, MonumentNorth, MonumentSouth, MonumentUp} {
		if r.Openings[d] {
			kind = MonumentSimple
			break
		}
	}
	r.claimed = true
	m.addRoom(kind, r, 1, 1, 1)
	if kind == MonumentSimple {
		m.Pieces[len(m.Pieces)-1].Design = int(rng.NextIntN(3))
	}
}
