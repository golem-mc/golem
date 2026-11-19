package world

// vanilla: OceanMonumentPieces.MonumentBuilding

func (p *monumentPlacer) wing(isFlipped bool, xoff int32) {
	if p.intersects(xoff, 0, xoff+23, 20) {
		p.box(xoff, 0, 0, xoff+24, 0, 20, p.gray, p.gray)
		p.waterBox(xoff, 1, 0, xoff+24, 10, 20)
		for i := range int32(4) {
			p.box(xoff+i, i+1, i, xoff+i, i+1, 20, p.light, p.light)
			p.box(xoff+i+7, i+5, i+7, xoff+i+7, i+5, 20, p.light, p.light)
			p.box(xoff+17-i, i+5, i+7, xoff+17-i, i+5, 20, p.light, p.light)
			p.box(xoff+24-i, i+1, i, xoff+24-i, i+1, 20, p.light, p.light)
			p.box(xoff+i+1, i+1, i, xoff+23-i, i+1, i, p.light, p.light)
			p.box(xoff+i+8, i+5, i+7, xoff+16-i, i+5, i+7, p.light, p.light)
		}
		p.box(xoff+4, 4, 4, xoff+6, 4, 20, p.gray, p.gray)
		p.box(xoff+7, 4, 4, xoff+17, 4, 6, p.gray, p.gray)
		p.box(xoff+18, 4, 4, xoff+20, 4, 20, p.gray, p.gray)
		p.box(xoff+11, 8, 11, xoff+13, 8, 20, p.gray, p.gray)
		p.put(xoff+12, 9, 12, p.light)
		p.put(xoff+12, 9, 15, p.light)
		p.put(xoff+12, 9, 18, p.light)
		leftPos, rightPos := xoff+5, xoff+19
		if isFlipped {
			leftPos, rightPos = rightPos, leftPos
		}
		for z := int32(20); z >= 5; z -= 3 {
			p.put(leftPos, 5, z, p.light)
		}
		for z := int32(19); z >= 7; z -= 3 {
			p.put(rightPos, 5, z, p.light)
		}
		for i := range int32(4) {
			pos := xoff + 17 - i*3
			if isFlipped {
				pos = xoff + 24 - (17 - i*3)
			}
			p.put(pos, 5, 5, p.light)
		}
		p.put(rightPos, 5, 5, p.light)
		p.box(xoff+11, 1, 12, xoff+13, 7, 12, p.gray, p.gray)
		p.box(xoff+12, 1, 11, xoff+12, 7, 13, p.gray, p.gray)
	}
}

func (p *monumentPlacer) entranceArchs() {
	if p.intersects(22, 5, 35, 17) {
		p.waterBox(25, 0, 0, 32, 8, 20)
		for i := range int32(4) {
			p.box(24, 2, 5+i*4, 24, 4, 5+i*4, p.light, p.light)
			p.box(22, 4, 5+i*4, 23, 4, 5+i*4, p.light, p.light)
			p.put(25, 5, 5+i*4, p.light)
			p.put(26, 6, 5+i*4, p.light)
			p.put(26, 5, 5+i*4, p.lamp)
			p.box(33, 2, 5+i*4, 33, 4, 5+i*4, p.light, p.light)
			p.box(34, 4, 5+i*4, 35, 4, 5+i*4, p.light, p.light)
			p.put(32, 5, 5+i*4, p.light)
			p.put(31, 6, 5+i*4, p.light)
			p.put(31, 5, 5+i*4, p.lamp)
			p.box(27, 6, 5+i*4, 30, 6, 5+i*4, p.gray, p.gray)
		}
	}
}

func (p *monumentPlacer) entranceWall() {
	if p.intersects(15, 20, 42, 21) {
		p.box(15, 0, 21, 42, 0, 21, p.gray, p.gray)
		p.waterBox(26, 1, 21, 31, 3, 21)
		p.box(21, 12, 21, 36, 12, 21, p.gray, p.gray)
		p.box(17, 11, 21, 40, 11, 21, p.gray, p.gray)
		p.box(16, 10, 21, 41, 10, 21, p.gray, p.gray)
		p.box(15, 7, 21, 42, 9, 21, p.gray, p.gray)
		p.box(16, 6, 21, 41, 6, 21, p.gray, p.gray)
		p.box(17, 5, 21, 40, 5, 21, p.gray, p.gray)
		p.box(21, 4, 21, 36, 4, 21, p.gray, p.gray)
		p.box(22, 3, 21, 26, 3, 21, p.gray, p.gray)
		p.box(31, 3, 21, 35, 3, 21, p.gray, p.gray)
		p.box(23, 2, 21, 25, 2, 21, p.gray, p.gray)
		p.box(32, 2, 21, 34, 2, 21, p.gray, p.gray)
		p.box(28, 4, 20, 29, 4, 21, p.light, p.light)
		p.put(27, 3, 21, p.light)
		p.put(30, 3, 21, p.light)
		p.put(26, 2, 21, p.light)
		p.put(31, 2, 21, p.light)
		p.put(25, 1, 21, p.light)
		p.put(32, 1, 21, p.light)
		for i := range int32(7) {
			p.put(28-i, 6+i, 21, p.black)
			p.put(29+i, 6+i, 21, p.black)
		}
		for i := range int32(4) {
			p.put(28-i, 9+i, 21, p.black)
			p.put(29+i, 9+i, 21, p.black)
		}
		p.put(28, 12, 21, p.black)
		p.put(29, 12, 21, p.black)
		for i := range int32(3) {
			p.put(22-i*2, 8, 21, p.black)
			p.put(22-i*2, 9, 21, p.black)
			p.put(35+i*2, 8, 21, p.black)
			p.put(35+i*2, 9, 21, p.black)
		}
		p.waterBox(15, 13, 21, 42, 15, 21)
		p.waterBox(15, 1, 21, 15, 6, 21)
		p.waterBox(16, 1, 21, 16, 5, 21)
		p.waterBox(17, 1, 21, 20, 4, 21)
		p.waterBox(21, 1, 21, 21, 3, 21)
		p.waterBox(22, 1, 21, 22, 2, 21)
		p.waterBox(23, 1, 21, 24, 1, 21)
		p.waterBox(42, 1, 21, 42, 6, 21)
		p.waterBox(41, 1, 21, 41, 5, 21)
		p.waterBox(37, 1, 21, 40, 4, 21)
		p.waterBox(36, 1, 21, 36, 3, 21)
		p.waterBox(33, 1, 21, 34, 1, 21)
		p.waterBox(35, 1, 21, 35, 2, 21)
	}
}

func (p *monumentPlacer) roofPiece() {
	if p.intersects(21, 21, 36, 36) {
		p.box(21, 0, 22, 36, 0, 36, p.gray, p.gray)
		p.waterBox(21, 1, 22, 36, 23, 36)
		for i := range int32(4) {
			p.box(21+i, 13+i, 21+i, 36-i, 13+i, 21+i, p.light, p.light)
			p.box(21+i, 13+i, 36-i, 36-i, 13+i, 36-i, p.light, p.light)
			p.box(21+i, 13+i, 22+i, 21+i, 13+i, 35-i, p.light, p.light)
			p.box(36-i, 13+i, 22+i, 36-i, 13+i, 35-i, p.light, p.light)
		}
		p.box(25, 16, 25, 32, 16, 32, p.gray, p.gray)
		p.box(25, 17, 25, 25, 19, 25, p.light, p.light)
		p.box(32, 17, 25, 32, 19, 25, p.light, p.light)
		p.box(25, 17, 32, 25, 19, 32, p.light, p.light)
		p.box(32, 17, 32, 32, 19, 32, p.light, p.light)
		p.put(26, 20, 26, p.light)
		p.put(27, 21, 27, p.light)
		p.put(27, 20, 27, p.lamp)
		p.put(26, 20, 31, p.light)
		p.put(27, 21, 30, p.light)
		p.put(27, 20, 30, p.lamp)
		p.put(31, 20, 31, p.light)
		p.put(30, 21, 30, p.light)
		p.put(30, 20, 30, p.lamp)
		p.put(31, 20, 26, p.light)
		p.put(30, 21, 27, p.light)
		p.put(30, 20, 27, p.lamp)
		p.box(28, 21, 27, 29, 21, 27, p.gray, p.gray)
		p.box(27, 21, 28, 27, 21, 29, p.gray, p.gray)
		p.box(28, 21, 30, 29, 21, 30, p.gray, p.gray)
		p.box(30, 21, 28, 30, 21, 29, p.gray, p.gray)
	}
}

func (p *monumentPlacer) lowerWall() {
	if p.intersects(0, 21, 6, 58) {
		p.box(0, 0, 21, 6, 0, 57, p.gray, p.gray)
		p.waterBox(0, 1, 21, 6, 7, 57)
		p.box(4, 4, 21, 6, 4, 53, p.gray, p.gray)
		for i := range int32(4) {
			p.box(i, i+1, 21, i, i+1, 57-i, p.light, p.light)
		}
		for z := int32(23); z < 53; z += 3 {
			p.put(5, 5, z, p.light)
		}
		p.put(5, 5, 52, p.light)
		for i := range int32(4) {
			p.box(i, i+1, 21, i, i+1, 57-i, p.light, p.light)
		}
		p.box(4, 1, 52, 6, 3, 52, p.gray, p.gray)
		p.box(5, 1, 51, 5, 3, 53, p.gray, p.gray)
	}
	if p.intersects(51, 21, 58, 58) {
		p.box(51, 0, 21, 57, 0, 57, p.gray, p.gray)
		p.waterBox(51, 1, 21, 57, 7, 57)
		p.box(51, 4, 21, 53, 4, 53, p.gray, p.gray)
		for i := range int32(4) {
			p.box(57-i, i+1, 21, 57-i, i+1, 57-i, p.light, p.light)
		}
		for z := int32(23); z < 53; z += 3 {
			p.put(52, 5, z, p.light)
		}
		p.put(52, 5, 52, p.light)
		p.box(51, 1, 52, 53, 3, 52, p.gray, p.gray)
		p.box(52, 1, 51, 52, 3, 53, p.gray, p.gray)
	}
	if p.intersects(0, 51, 57, 57) {
		p.box(7, 0, 51, 50, 0, 57, p.gray, p.gray)
		p.waterBox(7, 1, 51, 50, 10, 57)
		for i := range int32(4) {
			p.box(i+1, i+1, 57-i, 56-i, i+1, 57-i, p.light, p.light)
		}
	}
}

func (p *monumentPlacer) middleWall() {
	if p.intersects(7, 21, 13, 50) {
		p.box(7, 0, 21, 13, 0, 50, p.gray, p.gray)
		p.waterBox(7, 1, 21, 13, 10, 50)
		p.box(11, 8, 21, 13, 8, 53, p.gray, p.gray)
		for i := range int32(4) {
			p.box(i+7, i+5, 21, i+7, i+5, 54, p.light, p.light)
		}
		for z := int32(21); z <= 45; z += 3 {
			p.put(12, 9, z, p.light)
		}
	}
	if p.intersects(44, 21, 50, 54) {
		p.box(44, 0, 21, 50, 0, 50, p.gray, p.gray)
		p.waterBox(44, 1, 21, 50, 10, 50)
		p.box(44, 8, 21, 46, 8, 53, p.gray, p.gray)
		for i := range int32(4) {
			p.box(50-i, i+5, 21, 50-i, i+5, 54, p.light, p.light)
		}
		for z := int32(21); z <= 45; z += 3 {
			p.put(45, 9, z, p.light)
		}
	}
	if p.intersects(8, 44, 49, 54) {
		p.box(14, 0, 44, 43, 0, 50, p.gray, p.gray)
		p.waterBox(14, 1, 44, 43, 10, 50)
		for x := int32(12); x <= 45; x += 3 {
			p.put(x, 9, 45, p.light)
			p.put(x, 9, 52, p.light)
			if x != 12 && x != 18 && x != 24 && x != 33 && x != 39 && x != 45 {
				continue
			}
			p.put(x, 9, 47, p.light)
			p.put(x, 9, 50, p.light)
			p.put(x, 10, 45, p.light)
			p.put(x, 10, 46, p.light)
			p.put(x, 10, 51, p.light)
			p.put(x, 10, 52, p.light)
			p.put(x, 11, 47, p.light)
			p.put(x, 11, 50, p.light)
			p.put(x, 12, 48, p.light)
			p.put(x, 12, 49, p.light)
		}
		for i := range int32(3) {
			p.box(8+i, 5+i, 54, 49-i, 5+i, 54, p.gray, p.gray)
		}
		p.box(11, 8, 54, 46, 8, 54, p.light, p.light)
		p.box(14, 8, 44, 43, 8, 53, p.gray, p.gray)
	}
}

func (p *monumentPlacer) upperWall() {
	if p.intersects(14, 21, 20, 43) {
		p.box(14, 0, 21, 20, 0, 43, p.gray, p.gray)
		p.waterBox(14, 1, 22, 20, 14, 43)
		p.box(18, 12, 22, 20, 12, 39, p.gray, p.gray)
		p.box(18, 12, 21, 20, 12, 21, p.light, p.light)
		for i := range int32(4) {
			p.box(i+14, i+9, 21, i+14, i+9, 43-i, p.light, p.light)
		}
		for z := int32(23); z <= 39; z += 3 {
			p.put(19, 13, z, p.light)
		}
	}
	if p.intersects(37, 21, 43, 43) {
		p.box(37, 0, 21, 43, 0, 43, p.gray, p.gray)
		p.waterBox(37, 1, 22, 43, 14, 43)
		p.box(37, 12, 22, 39, 12, 39, p.gray, p.gray)
		p.box(37, 12, 21, 39, 12, 21, p.light, p.light)
		for i := range int32(4) {
			p.box(43-i, i+9, 21, 43-i, i+9, 43-i, p.light, p.light)
		}
		for z := int32(23); z <= 39; z += 3 {
			p.put(38, 13, z, p.light)
		}
	}
	if p.intersects(15, 37, 42, 43) {
		p.box(21, 0, 37, 36, 0, 43, p.gray, p.gray)
		p.waterBox(21, 1, 37, 36, 14, 43)
		p.box(21, 12, 37, 36, 12, 39, p.gray, p.gray)
		for i := range int32(4) {
			p.box(15+i, i+9, 43-i, 42-i, i+9, 43-i, p.light, p.light)
		}
		for x := int32(21); x <= 36; x += 3 {
			p.put(x, 13, 38, p.light)
		}
	}
}
