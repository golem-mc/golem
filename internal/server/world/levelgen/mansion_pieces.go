package levelgen

// MansionPiece is a woodland_mansion/ template and its world transform.
// Rotation is 0..3 clockwise quarter turns; Mirror is 0 (none), 1 (left/right)
// or 2 (front/back), applied before rotation, as in StructurePlaceSettings.
type MansionPiece struct {
	Name             string
	Pos              [3]int
	Rotation, Mirror int
}

type mansionPos [3]int

func (p mansionPos) offset(rotation, east, up, south int) mansionPos {
	switch rotation & 3 {
	case 1:
		east, south = -south, east
	case 2:
		east, south = -east, -south
	case 3:
		east, south = south, -east
	}
	return mansionPos{p[0] + east, p[1] + up, p[2] + south}
}

type mansionPlacer struct {
	rng    RandomSource
	pieces []MansionPiece
}

func (p *mansionPlacer) add(name string, pos mansionPos, rotation, mirror int) {
	p.pieces = append(p.pieces, MansionPiece{name, pos, rotation & 3, mirror})
}

// GenerateMansion assembles the ordered templates at origin. The random stream
// starts before the floor grids and ends after the last room-template pick.
// vanilla: WoodlandMansionPieces.generateMansion, MansionPiecePlacer.createMansion
func GenerateMansion(rng RandomSource, origin [3]int, rotation int) []MansionPiece {
	g := NewMansionGrid(rng)
	p := &mansionPlacer{rng: rng}
	pos := mansionPos(origin)
	p.add("entrance", pos.offset(rotation, -9, 0, 0), rotation, 0)
	wallPos, wallRot := p.outerWalls(pos.offset(rotation, 0, 0, 16), rotation, "wall_flat", &g.Base, 8, 5, 8, 4)
	p.outerWalls(pos.offset(rotation, 0, 8, 16), rotation, "wall_window", &g.Base, 8, 5, 8, 4)
	found := false
	for z := 0; z < 11 && !found; z++ {
		for x := 10; x >= 0 && !found; x-- {
			if g.Third.IsHouse(x, z) {
				third := wallPos.offset(rotation, (x-8)*8, 19, 8+(z-5)*8)
				p.add("wall_window", third.offset(wallRot, 7, 0, 0), wallRot, 0)
				p.outerWalls(third.offset(wallRot, 0, 0, 8), wallRot, "wall_window", &g.Third, x, z, x, z)
				found = true
			}
		}
	}
	p.roof(pos.offset(0, 0, 16, 0), rotation, &g.Base, &g.Third)
	p.roof(pos.offset(0, 0, 27, 0), rotation, &g.Third, nil)
	for floor := range 3 {
		up := floor * 8
		grid := &g.Base
		if floor == 2 {
			up += 3
			grid = &g.Third
		}
		p.floor(pos.offset(0, 0, up, 0), rotation, floor, grid, &g.Rooms[floor])
	}
	return p.pieces
}

// vanilla: MansionPiecePlacer.traverseOuterWalls, traverseWallPiece,
// traverseTurn, traverseInnerTurn
func (p *mansionPlacer) outerWalls(pos mansionPos, rot int, wall string, grid *MansionFloor, x, z, endX, endZ int) (mansionPos, int) {
	d := dS
	for {
		dx, dz := mansionDX[d], mansionDZ[d]
		ccw := d.counterClockwise()
		switch {
		case !grid.IsHouse(x+dx, z+dz):
			pos = pos.offset(rot, 0, 0, -1)
			p.add("wall_corner", pos, rot, 0)
			pos = pos.offset(rot, 6, 0, -7)
			rot = (rot + 1) & 3
			d = d.clockwise()
			if x != endX || z != endZ || d != dS {
				p.add(wall, pos.offset(rot, 7, 0, 0), rot, 0)
				pos = pos.offset(rot, 0, 0, 8)
			}
		case grid.IsHouse(x+dx+mansionDX[ccw], z+dz+mansionDZ[ccw]):
			pos = pos.offset(rot, 8, 0, 6)
			rot = (rot + 3) & 3
			x, z, d = x+dx, z+dz, ccw
		default:
			x, z = x+dx, z+dz
			if x != endX || z != endZ || d != dS {
				p.add(wall, pos.offset(rot, 7, 0, 0), rot, 0)
				pos = pos.offset(rot, 0, 0, 8)
			}
		}
		if x == endX && z == endZ && d == dS {
			return pos, rot
		}
	}
}

// vanilla: MansionPiecePlacer.createRoof
func (p *mansionPlacer) roof(origin mansionPos, rot int, grid, above *MansionFloor) {
	for pass := range 3 {
		for z := range 11 {
			for x := range 11 {
				if !grid.IsHouse(x, z) {
					continue
				}
				isAbove := above != nil && above.IsHouse(x, z)
				pos := origin.offset(rot, (x-8)*8, 0, 8+(z-5)*8)
				add := func(name string, east, up, south, turn int) {
					p.add(name, pos.offset(rot, east, up, south), rot+turn, 0)
				}
				e, w, n, s := grid.IsHouse(x+1, z), grid.IsHouse(x-1, z), grid.IsHouse(x, z-1), grid.IsHouse(x, z+1)
				switch pass {
				case 0:
					if isAbove {
						continue
					}
					add("roof", 0, 3, 0, 0)
					if !e {
						add("roof_front", 6, 0, 0, 0)
					}
					if !w {
						add("roof_front", 0, 0, 7, 2)
					}
					if !n {
						add("roof_front", -1, 0, 0, 3)
					}
					if !s {
						add("roof_front", 6, 0, 6, 1)
					}
				case 1:
					if !isAbove {
						continue
					}
					if !e {
						add("small_wall", 7, 0, 0, 0)
					}
					if !w {
						add("small_wall", -1, 0, 6, 2)
					}
					if !n {
						add("small_wall", 0, 0, -1, 3)
					}
					if !s {
						add("small_wall", 6, 0, 7, 1)
					}
					if !e && !n {
						add("small_wall_corner", 7, 0, -2, 0)
					}
					if !e && !s {
						add("small_wall_corner", 8, 0, 7, 1)
					}
					if !w && !n {
						add("small_wall_corner", -2, 0, -1, 3)
					}
					if !w && !s {
						add("small_wall_corner", -1, 0, 8, 2)
					}
				case 2:
					if isAbove {
						continue
					}
					if !e {
						if !s {
							add("roof_corner", 6, 0, 6, 0)
						} else if grid.IsHouse(x+1, z+1) {
							add("roof_inner_corner", 6, 0, 5, 0)
						}
						if !n {
							add("roof_corner", 6, 0, 0, 3)
						} else if grid.IsHouse(x+1, z-1) {
							add("roof_inner_corner", 9, 0, -2, 1)
						}
					}
					if !w {
						if !s {
							add("roof_corner", 0, 0, 6, 1)
						} else if grid.IsHouse(x-1, z+1) {
							add("roof_inner_corner", -3, 0, 8, 3)
						}
						if !n {
							add("roof_corner", 0, 0, 0, 2)
						} else if grid.IsHouse(x-1, z-1) {
							add("roof_inner_corner", 0, 0, 1, 2)
						}
					}
				}
			}
		}
	}
}

// vanilla: MansionPiecePlacer.createMansion (corridors, walls and rooms)
func (p *mansionPlacer) floor(origin mansionPos, rot, floor int, grid, rooms *MansionFloor) {
	south, west, wall, door := "carpet_south_2", "carpet_west_2", "indoors_wall_2", "indoors_door_2"
	if floor == 0 {
		south, west, wall, door = "carpet_south_1", "carpet_west_1", "indoors_wall_1", "indoors_door_1"
	}
	for z := range 11 {
		for x := range 11 {
			if grid.get(x, z) != 1 {
				continue
			}
			pos := origin.offset(rot, (x-8)*8, 0, 8+(z-5)*8)
			p.add("corridor_floor", pos, rot, 0)
			connects := func(x, z int) bool { return grid.get(x, z) == 1 || rooms.get(x, z)&MansionRoomCorridor != 0 }
			if connects(x, z-1) {
				p.add("carpet_north", pos.offset(rot, 1, 1, 0), rot, 0)
			}
			if connects(x+1, z) {
				p.add("carpet_east", pos.offset(rot, 5, 1, 1), rot, 0)
			}
			if connects(x, z+1) {
				p.add(south, pos.offset(rot, -1, 0, 5), rot, 0)
			}
			if connects(x-1, z) {
				p.add(west, pos.offset(rot, -1, 0, -1), rot, 0)
			}
		}
	}
	for z := range 11 {
		for x := range 11 {
			thirdStart := floor == 2 && grid.get(x, z) == 3
			if grid.get(x, z) != 2 && !thirdStart {
				continue
			}
			data := rooms.get(x, z)
			typ, id := data&MansionRoomTypeMask, data&MansionRoomIDMask
			thirdStart = thirdStart && data&MansionRoomCorridor != 0
			var dirs []fdir
			if data&MansionRoomDoor != 0 {
				for d := dN; d <= dW; d++ {
					if grid.get(x+mansionDX[d], z+mansionDZ[d]) == 1 {
						dirs = append(dirs, d)
					}
				}
			}
			// -1 is no entrance; 4 is the UP sentinel for a secret room.
			entrance := -1
			if len(dirs) != 0 {
				entrance = int(dirs[p.rng.NextIntN(int32(len(dirs)))])
			} else if data&MansionRoomOrigin != 0 {
				entrance = 4
			}
			pos := origin.offset(rot, -1+(x-8)*8, 0, 8+(z-5)*8)
			same := func(x, z int) bool { return rooms.get(x, z)&MansionRoomIDMask == id }
			addWall := func(d fdir, east, south, turn int) {
				name := wall
				if entrance == int(d) {
					name = door
				}
				p.add(name, pos.offset(rot, east, 0, south), rot+turn, 0)
			}
			if grid.IsHouse(x-1, z) && !same(x-1, z) {
				addWall(dW, 0, 0, 0)
			}
			if grid.get(x+1, z) == 1 && !thirdStart {
				addWall(dE, 8, 0, 0)
			}
			if grid.IsHouse(x, z+1) && !same(x, z+1) {
				addWall(dS, 7, 7, 1)
			}
			if grid.get(x, z-1) == 1 && !thirdStart {
				addWall(dN, 7, -1, 1)
			}
			switch {
			case typ == MansionRoom1x1:
				p.room1x1(pos, rot, entrance, floor)
			case typ == MansionRoom1x2 && entrance != -1:
				roomDir := -1
				for d := dN; d <= dW; d++ {
					if same(x+mansionDX[d], z+mansionDZ[d]) {
						roomDir = int(d)
						break
					}
				}
				p.room1x2(pos, rot, roomDir, entrance, floor, data&MansionRoomStairs != 0)
			case typ == MansionRoom2x2 && entrance >= 0 && entrance < 4:
				d := fdir(entrance).clockwise()
				if !same(x+mansionDX[d], z+mansionDZ[d]) {
					d = d.opposite()
				}
				p.room2x2(pos, rot, int(d), entrance, floor)
			case typ == MansionRoom2x2 && entrance == 4:
				p.add(MansionRoomTemplate(p.rng, floor, MansionBigSecret, false), pos.offset(rot, 1, 0, 0), rot, 0)
			}
		}
	}
}

// vanilla: MansionPiecePlacer.addRoom1x1
func (p *mansionPlacer) room1x1(pos mansionPos, rot, door, floor int) {
	// The normal pick consumes a draw even when a secret template replaces it.
	name := MansionRoomTemplate(p.rng, floor, MansionSmallRoom, false)
	// StructureTemplate.getZeroPositionWithTransform at (1,0,0) for a 7x7 room.
	turn, east, south := 0, 1, 0
	switch door {
	case int(dE):
	case int(dN):
		turn, south = 3, 6
	case int(dW):
		turn, east, south = 2, 7, 6
	case int(dS):
		turn, east = 1, 7
	default:
		name = MansionRoomTemplate(p.rng, floor, MansionSmallSecret, false)
	}
	p.add(name, pos.offset(rot, east, 0, south), rot+turn, 0)
}

// vanilla: MansionPiecePlacer.addRoom1x2
func (p *mansionPlacer) room1x2(pos mansionPos, rot, room, door, floor int, stairs bool) {
	east, south, turn, mirror := 0, 0, 0, 0
	kind := MansionMediumSide
	switch [2]int{door, room} {
	case [2]int{int(dE), int(dS)}:
		east = 1
	case [2]int{int(dE), int(dN)}:
		east, south, mirror = 1, 6, 1
	case [2]int{int(dW), int(dN)}:
		east, south, turn = 7, 6, 2
	case [2]int{int(dW), int(dS)}:
		east, mirror = 7, 2
	case [2]int{int(dS), int(dE)}:
		east, turn, mirror = 1, 1, 1
	case [2]int{int(dS), int(dW)}:
		east, turn = 7, 1
	case [2]int{int(dN), int(dW)}:
		east, south, turn, mirror = 7, 6, 1, 2
	case [2]int{int(dN), int(dE)}:
		east, south, turn = 1, 6, 3
	case [2]int{int(dS), int(dN)}:
		east, south, kind = 1, -8, MansionMediumFront
	case [2]int{int(dN), int(dS)}:
		east, south, turn, kind = 7, 14, 2, MansionMediumFront
	case [2]int{int(dW), int(dE)}:
		east, turn, kind = 15, 1, MansionMediumFront
	case [2]int{int(dE), int(dW)}:
		east, south, turn, kind = -7, 6, 3, MansionMediumFront
	case [2]int{4, int(dE)}:
		east, turn, kind = 15, 1, MansionMediumSecret
	case [2]int{4, int(dS)}:
		east, kind = 1, MansionMediumSecret
	default:
		return
	}
	p.add(MansionRoomTemplate(p.rng, floor, kind, stairs), pos.offset(rot, east, 0, south), rot+turn, mirror)
}

// vanilla: MansionPiecePlacer.addRoom2x2
func (p *mansionPlacer) room2x2(pos mansionPos, rot, room, door, floor int) {
	east, south, turn, mirror := 0, 0, 0, 0
	switch [2]int{door, room} {
	case [2]int{int(dE), int(dS)}:
		east = -7
	case [2]int{int(dE), int(dN)}:
		east, south, mirror = -7, 6, 1
	case [2]int{int(dN), int(dE)}:
		east, south, turn = 1, 14, 3
	case [2]int{int(dN), int(dW)}:
		east, south, turn, mirror = 7, 14, 3, 1
	case [2]int{int(dS), int(dW)}:
		east, south, turn = 7, -8, 1
	case [2]int{int(dS), int(dE)}:
		east, south, turn, mirror = 1, -8, 1, 1
	case [2]int{int(dW), int(dN)}:
		east, south, turn = 15, 6, 2
	case [2]int{int(dW), int(dS)}:
		east, mirror = 15, 2
	}
	p.add(MansionRoomTemplate(p.rng, floor, MansionBigRoom, false), pos.offset(rot, east, 0, south), rot+turn, mirror)
}
