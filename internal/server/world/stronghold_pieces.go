package world

// The postProcess of each stronghold piece, in piece-local coordinates.
// vanilla: StrongholdPieces

func (p *strongholdPlacer) straight(door string, left, right int) {
	s := p.st
	p.stoneBox(0, 0, 0, 4, 4, 6, true)
	p.smallDoor(door, 1, 1, 0)
	p.smallDoor("OPENING", 1, 1, 6)
	p.maybe(0.1, 1, 2, 1, s.wallTorch[p.mapDir(shE)])
	p.maybe(0.1, 3, 2, 1, s.wallTorch[p.mapDir(shW)])
	p.maybe(0.1, 1, 2, 5, s.wallTorch[p.mapDir(shE)])
	p.maybe(0.1, 3, 2, 5, s.wallTorch[p.mapDir(shW)])
	if left != 0 {
		p.air(0, 1, 2, 0, 3, 4)
	}
	if right != 0 {
		p.air(4, 1, 2, 4, 3, 4)
	}
}

func (p *strongholdPlacer) prisonHall(door string) {
	s := p.st
	p.stoneBox(0, 0, 0, 8, 4, 10, true)
	p.smallDoor(door, 1, 1, 0)
	p.air(1, 1, 10, 3, 3, 10)
	for _, z := range [...]int32{1, 3, 7, 9} {
		p.stoneBox(4, 1, z, 4, 3, z, false)
	}
	for y := int32(1); y <= 3; y++ {
		p.mask(4, y, 4, &s.bars, 1<<shN|1<<shS)
		p.mask(4, y, 5, &s.bars, 1<<shN|1<<shS|1<<shE)
		p.mask(4, y, 6, &s.bars, 1<<shN|1<<shS)
		p.mask(5, y, 5, &s.bars, 1<<shW|1<<shE)
		p.mask(6, y, 5, &s.bars, 1<<shW|1<<shE)
		p.mask(7, y, 5, &s.bars, 1<<shW|1<<shE)
	}
	p.mask(4, 3, 2, &s.bars, 1<<shN|1<<shS)
	p.mask(4, 3, 8, &s.bars, 1<<shN|1<<shS)
	p.door(4, 1, 2, 1, shW)
	p.door(4, 1, 8, 1, shW)
}

// turn places a left or right turn; its corridor leaves by the wall at
// piece-local x 0 when nearWall, else by the one at x 4.
func (p *strongholdPlacer) turn(door string, nearWall bool) {
	p.stoneBox(0, 0, 0, 4, 4, 4, true)
	p.smallDoor(door, 1, 1, 0)
	if nearWall {
		p.air(0, 1, 1, 0, 3, 3)
	} else {
		p.air(4, 1, 1, 4, 3, 3)
	}
}

func (p *strongholdPlacer) roomCrossing(door string, kind int) {
	s := p.st
	p.stoneBox(0, 0, 0, 10, 6, 10, true)
	p.smallDoor(door, 4, 1, 0)
	p.air(4, 1, 10, 6, 3, 10)
	p.air(0, 1, 4, 0, 3, 6)
	p.air(10, 1, 4, 10, 3, 6)
	switch kind {
	case 0:
		p.solid(5, 1, 5, 5, 3, 5, s.bricks)
		p.facing(4, 3, 5, &s.wallTorch, shW)
		p.facing(6, 3, 5, &s.wallTorch, shE)
		p.facing(5, 3, 4, &s.wallTorch, shS)
		p.facing(5, 3, 6, &s.wallTorch, shN)
		for _, o := range [...][2]int32{{4, 4}, {4, 5}, {4, 6}, {6, 4}, {6, 5}, {6, 6}, {5, 4}, {5, 6}} {
			p.put(o[0], 1, o[1], s.smoothSlab)
		}
	case 1:
		for i := range int32(5) {
			p.put(3, 1, 3+i, s.bricks)
			p.put(7, 1, 3+i, s.bricks)
			p.put(3+i, 1, 3, s.bricks)
			p.put(3+i, 1, 7, s.bricks)
		}
		p.solid(5, 1, 5, 5, 3, 5, s.bricks)
		p.put(5, 4, 5, s.water)
	case 2:
		for z := int32(1); z <= 9; z++ {
			p.put(1, 3, z, s.cobble)
			p.put(9, 3, z, s.cobble)
		}
		for x := int32(1); x <= 9; x++ {
			p.put(x, 3, 1, s.cobble)
			p.put(x, 3, 9, s.cobble)
		}
		for _, o := range [...][3]int32{{5, 1, 4}, {5, 1, 6}, {5, 3, 4}, {5, 3, 6}, {4, 1, 5}, {6, 1, 5}, {4, 3, 5}, {6, 3, 5}} {
			p.put(o[0], o[1], o[2], s.cobble)
		}
		for y := int32(1); y <= 3; y++ {
			p.put(4, y, 4, s.cobble)
			p.put(6, y, 4, s.cobble)
			p.put(4, y, 6, s.cobble)
			p.put(6, y, 6, s.cobble)
		}
		p.facing(5, 3, 5, &s.wallTorch, shN)
		for z := int32(2); z <= 8; z++ {
			p.put(2, 3, z, s.planks)
			p.put(3, 3, z, s.planks)
			if z <= 3 || z >= 7 {
				p.put(4, 3, z, s.planks)
				p.put(5, 3, z, s.planks)
				p.put(6, 3, z, s.planks)
			}
			p.put(7, 3, z, s.planks)
			p.put(8, 3, z, s.planks)
		}
		for y := int32(1); y <= 3; y++ {
			p.facing(9, y, 3, &s.ladder, shW)
		}
		p.chestAt(3, 4, 8, shCrossingLoot)
	}
}

func (p *strongholdPlacer) straightStairsDown(door string) {
	s := p.st
	p.stoneBox(0, 0, 0, 4, 10, 7, true)
	p.smallDoor(door, 1, 7, 0)
	p.smallDoor("OPENING", 1, 1, 7)
	for i := range int32(6) {
		for x := int32(1); x <= 3; x++ {
			p.facing(x, 6-i, 1+i, &s.cobbleStairs, shS)
		}
		if i >= 5 {
			continue
		}
		for x := int32(1); x <= 3; x++ {
			p.put(x, 5-i, 1+i, s.bricks)
		}
	}
}

func (p *strongholdPlacer) stairsDown(door string) {
	s := p.st
	p.stoneBox(0, 0, 0, 4, 10, 4, true)
	p.smallDoor(door, 1, 7, 0)
	p.smallDoor("OPENING", 1, 1, 4)
	for _, o := range [...][3]int32{
		{2, 6, 1}, {1, 5, 1}, {1, 5, 2}, {1, 4, 3}, {2, 4, 3}, {3, 3, 3}, {3, 3, 2}, {3, 2, 1}, {2, 2, 1}, {1, 1, 1}, {1, 1, 2},
	} {
		p.put(o[0], o[1], o[2], s.bricks)
	}
	for _, o := range [...][3]int32{{1, 6, 1}, {1, 5, 3}, {3, 4, 3}, {3, 3, 1}, {1, 2, 1}, {1, 1, 3}} {
		p.put(o[0], o[1], o[2], s.smoothSlab)
	}
}

func (p *strongholdPlacer) fiveCrossing(door string, leftLow, leftHigh, rightLow, rightHigh bool) {
	s := p.st
	p.stoneBox(0, 0, 0, 9, 8, 10, true)
	p.smallDoor(door, 4, 3, 0)
	if leftLow {
		p.air(0, 3, 1, 0, 5, 3)
	}
	if rightLow {
		p.air(9, 3, 1, 9, 5, 3)
	}
	if leftHigh {
		p.air(0, 5, 7, 0, 7, 9)
	}
	if rightHigh {
		p.air(9, 5, 7, 9, 7, 9)
	}
	p.air(5, 1, 10, 7, 3, 10)
	p.stoneBox(1, 2, 1, 8, 2, 6, false)
	p.stoneBox(4, 1, 5, 4, 4, 9, false)
	p.stoneBox(8, 1, 5, 8, 4, 9, false)
	p.stoneBox(1, 4, 7, 3, 4, 9, false)
	p.stoneBox(1, 3, 5, 3, 3, 6, false)
	p.solid(1, 3, 4, 3, 3, 4, s.smoothSlab)
	p.solid(1, 4, 6, 3, 4, 6, s.smoothSlab)
	p.stoneBox(5, 1, 7, 7, 1, 8, false)
	p.solid(5, 1, 9, 7, 1, 9, s.smoothSlab)
	p.solid(5, 2, 7, 7, 2, 7, s.smoothSlab)
	p.solid(4, 5, 7, 4, 5, 9, s.smoothSlab)
	p.solid(8, 5, 7, 8, 5, 9, s.smoothSlab)
	p.solid(5, 5, 7, 7, 5, 9, s.smoothDouble)
	p.facing(6, 5, 6, &s.wallTorch, shS)
}

func (p *strongholdPlacer) chestCorridor(door string) {
	s := p.st
	p.stoneBox(0, 0, 0, 4, 4, 6, true)
	p.smallDoor(door, 1, 1, 0)
	p.smallDoor("OPENING", 1, 1, 6)
	p.solid(3, 1, 2, 3, 1, 4, s.bricks)
	p.put(3, 1, 1, s.brickSlab)
	p.put(3, 1, 5, s.brickSlab)
	p.put(3, 2, 2, s.brickSlab)
	p.put(3, 2, 4, s.brickSlab)
	for z := int32(2); z <= 4; z++ {
		p.put(2, 1, z, s.brickSlab)
	}
	if wx, wy, wz := p.worldPos(3, 2, 3); p.chunk.inside(wx, wy, wz) {
		p.chestAt(3, 2, 3, shCorridorLoot)
	}
}

func (p *strongholdPlacer) library(door string, tall bool) {
	s := p.st
	height := int32(6)
	if tall {
		height = 11
	}
	p.stoneBox(0, 0, 0, 13, height-1, 14, true)
	p.smallDoor(door, 4, 1, 0)
	for y := int32(1); y <= 4; y++ {
		for x := int32(2); x <= 11; x++ {
			for z := int32(1); z <= 13; z++ {
				if p.rng.NextFloat() > 0.07 {
					continue
				}
				p.put(x, y, z, s.cobweb)
			}
		}
	}
	for d := int32(1); d <= 13; d++ {
		if (d-1)%4 == 0 {
			p.solid(1, 1, d, 1, 4, d, s.planks)
			p.solid(12, 1, d, 12, 4, d, s.planks)
			p.facing(2, 3, d, &s.wallTorch, shE)
			p.facing(11, 3, d, &s.wallTorch, shW)
			if tall {
				p.solid(1, 6, d, 1, 9, d, s.planks)
				p.solid(12, 6, d, 12, 9, d, s.planks)
			}
			continue
		}
		p.solid(1, 1, d, 1, 4, d, s.bookshelf)
		p.solid(12, 1, d, 12, 4, d, s.bookshelf)
		if tall {
			p.solid(1, 6, d, 1, 9, d, s.bookshelf)
			p.solid(12, 6, d, 12, 9, d, s.bookshelf)
		}
	}
	for d := int32(3); d < 12; d += 2 {
		p.solid(3, 1, d, 4, 3, d, s.bookshelf)
		p.solid(6, 1, d, 7, 3, d, s.bookshelf)
		p.solid(9, 1, d, 10, 3, d, s.bookshelf)
	}
	if tall {
		p.solid(1, 5, 1, 3, 5, 13, s.planks)
		p.solid(10, 5, 1, 12, 5, 13, s.planks)
		p.solid(4, 5, 1, 9, 5, 2, s.planks)
		p.solid(4, 5, 12, 9, 5, 13, s.planks)
		p.put(9, 5, 11, s.planks)
		p.put(8, 5, 11, s.planks)
		p.put(9, 5, 10, s.planks)
		const ns, we = 1<<shN | 1<<shS, 1<<shW | 1<<shE
		fence := func(x, y, z int32, mask int) { p.mask(x, y, z, &s.fence, mask) }
		fenceBox := func(x0, y0, z0, x1, y1, z1 int32, mask int) {
			st := s.fence[p.mapMask(mask)]
			p.solid(x0, y0, z0, x1, y1, z1, st)
		}
		fenceBox(3, 6, 3, 3, 6, 11, ns)
		fenceBox(10, 6, 3, 10, 6, 9, ns)
		fenceBox(4, 6, 2, 9, 6, 2, we)
		fenceBox(4, 6, 12, 7, 6, 12, we)
		fence(3, 6, 2, 1<<shN|1<<shE)
		fence(3, 6, 12, 1<<shS|1<<shE)
		fence(10, 6, 2, 1<<shN|1<<shW)
		for i := int32(0); i <= 2; i++ {
			fence(8+i, 6, 12-i, 1<<shS|1<<shW)
			if i == 2 {
				continue
			}
			fence(8+i, 6, 11-i, 1<<shN|1<<shE)
		}
		for y := int32(1); y <= 7; y++ {
			p.facing(10, y, 13, &s.ladder, shS)
		}
		const e, w = 1 << shE, 1 << shW
		fence(6, 9, 7, e)
		fence(7, 9, 7, w)
		fence(6, 8, 7, e)
		fence(7, 8, 7, w)
		fence(6, 7, 7, ns|we)
		fence(7, 7, 7, ns|we)
		fence(5, 7, 7, e)
		fence(8, 7, 7, w)
		fence(6, 7, 6, e|1<<shN)
		fence(6, 7, 8, e|1<<shS)
		fence(7, 7, 6, w|1<<shN)
		fence(7, 7, 8, w|1<<shS)
		for _, o := range [...][3]int32{{5, 8, 7}, {8, 8, 7}, {6, 8, 6}, {6, 8, 8}, {7, 8, 6}, {7, 8, 8}} {
			p.put(o[0], o[1], o[2], s.torch)
		}
	}
	p.chestAt(3, 3, 5, shLibraryLoot)
	if tall {
		p.put(12, 9, 1, s.caveAir)
		p.chestAt(12, 8, 1, shLibraryLoot)
	}
}

func (p *strongholdPlacer) portalRoom() {
	s := p.st
	p.stoneBox(0, 0, 0, 10, 7, 15, false)
	p.smallDoor("GRATES", 4, 1, 0)
	p.stoneBox(1, 6, 1, 1, 6, 14, false)
	p.stoneBox(9, 6, 1, 9, 6, 14, false)
	p.stoneBox(2, 6, 1, 8, 6, 2, false)
	p.stoneBox(2, 6, 14, 8, 6, 14, false)
	p.stoneBox(1, 1, 1, 2, 1, 4, false)
	p.stoneBox(8, 1, 1, 9, 1, 4, false)
	p.solid(1, 1, 1, 1, 1, 3, s.lava)
	p.solid(9, 1, 1, 9, 1, 3, s.lava)
	p.stoneBox(3, 1, 8, 7, 1, 12, false)
	p.solid(4, 1, 9, 6, 1, 11, s.lava)
	for z := int32(3); z < 14; z += 2 {
		p.box(0, 3, z, 0, 4, z, s.bars[p.mapMask(1<<shN|1<<shS)], s.bars[p.mapMask(1<<shN|1<<shS)])
		p.box(10, 3, z, 10, 4, z, s.bars[p.mapMask(1<<shN|1<<shS)], s.bars[p.mapMask(1<<shN|1<<shS)])
	}
	for x := int32(2); x < 9; x += 2 {
		p.box(x, 3, 15, x, 4, 15, s.bars[p.mapMask(1<<shW|1<<shE)], s.bars[p.mapMask(1<<shW|1<<shE)])
	}
	p.stoneBox(4, 1, 5, 6, 1, 7, false)
	p.stoneBox(4, 2, 6, 6, 2, 7, false)
	p.stoneBox(4, 3, 7, 6, 3, 7, false)
	for x := int32(4); x <= 6; x++ {
		p.facing(x, 1, 4, &s.brickStairs, shN)
		p.facing(x, 2, 5, &s.brickStairs, shN)
		p.facing(x, 3, 6, &s.brickStairs, shN)
	}
	allEyes := true
	var eyes [12]bool
	for i := range eyes {
		eyes[i] = p.rng.NextFloat() > 0.9
		allEyes = allEyes && eyes[i]
	}
	frame := func(x, z int32, local int, eye bool) {
		t := &s.frame
		if eye {
			t = &s.frameEye
		}
		p.facing(x, 3, z, t, local)
	}
	frame(4, 8, shN, eyes[0])
	frame(5, 8, shN, eyes[1])
	frame(6, 8, shN, eyes[2])
	frame(4, 12, shS, eyes[3])
	frame(5, 12, shS, eyes[4])
	frame(6, 12, shS, eyes[5])
	frame(3, 9, shE, eyes[6])
	frame(3, 10, shE, eyes[7])
	frame(3, 11, shE, eyes[8])
	frame(7, 9, shW, eyes[9])
	frame(7, 10, shW, eyes[10])
	frame(7, 11, shW, eyes[11])
	if allEyes {
		for x := int32(4); x <= 6; x++ {
			for z := int32(9); z <= 11; z++ {
				p.put(x, 3, z, s.portal)
			}
		}
	}
	if wx, wy, wz := p.worldPos(5, 3, 6); p.chunk.inside(wx, wy, wz) {
		p.r.set(wx, wy, wz, s.spawner)
		p.r.placeBlockEntity(wx, wy, wz, s.spawner, freshSpawnerTag("minecraft:silverfish"))
	}
}

func (p *strongholdPlacer) filler(steps int) {
	s := p.st
	for i := int32(0); i < int32(steps); i++ {
		for x := int32(0); x <= 4; x++ {
			p.put(x, 0, i, s.bricks)
		}
		for y := int32(1); y <= 3; y++ {
			p.put(0, y, i, s.bricks)
			p.put(1, y, i, s.caveAir)
			p.put(2, y, i, s.caveAir)
			p.put(3, y, i, s.caveAir)
			p.put(4, y, i, s.bricks)
		}
		for x := int32(0); x <= 4; x++ {
			p.put(x, 4, i, s.bricks)
		}
	}
}
