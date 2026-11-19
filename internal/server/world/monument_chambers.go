package world

// vanilla: OceanMonumentPieces.OceanMonumentPenthouse.postProcess
func (p *monumentPlacer) penthouse() {
	p.box(2, -1, 2, 11, -1, 11, p.light, p.light)
	p.box(0, -1, 0, 1, -1, 11, p.gray, p.gray)
	p.box(12, -1, 0, 13, -1, 11, p.gray, p.gray)
	p.box(2, -1, 0, 11, -1, 1, p.gray, p.gray)
	p.box(2, -1, 12, 11, -1, 13, p.gray, p.gray)
	p.box(0, 0, 0, 0, 0, 13, p.light, p.light)
	p.box(13, 0, 0, 13, 0, 13, p.light, p.light)
	p.box(1, 0, 0, 12, 0, 0, p.light, p.light)
	p.box(1, 0, 13, 12, 0, 13, p.light, p.light)
	for i := int32(2); i <= 11; i += 3 {
		p.put(0, 0, i, p.lamp)
		p.put(13, 0, i, p.lamp)
		p.put(i, 0, 0, p.lamp)
	}
	p.box(2, 0, 3, 4, 0, 9, p.light, p.light)
	p.box(9, 0, 3, 11, 0, 9, p.light, p.light)
	p.box(4, 0, 9, 9, 0, 11, p.light, p.light)
	p.put(5, 0, 8, p.light)
	p.put(8, 0, 8, p.light)
	p.put(10, 0, 10, p.light)
	p.put(3, 0, 10, p.light)
	p.box(3, 0, 3, 3, 0, 7, p.black, p.black)
	p.box(10, 0, 3, 10, 0, 7, p.black, p.black)
	p.box(6, 0, 10, 7, 0, 10, p.black, p.black)
	x := int32(3)
	for range 2 {
		for z := int32(2); z <= 8; z += 3 {
			p.box(x, 0, z, x, 2, z, p.light, p.light)
		}
		x = 10
	}
	p.box(5, 0, 10, 5, 2, 10, p.light, p.light)
	p.box(8, 0, 10, 8, 2, 10, p.light, p.light)
	p.box(6, -1, 7, 7, -1, 8, p.black, p.black)
	p.waterBox(6, -1, 3, 7, -1, 4)
	p.spawnElder(6, 1, 6)
}

// vanilla: OceanMonumentPieces.OceanMonumentWingRoom.postProcess
func (p *monumentPlacer) wingRoom(design int) {
	switch design {
	case 0:
		for i := range int32(4) {
			p.box(10-i, 3-i, 20-i, 12+i, 3-i, 20, p.light, p.light)
		}
		p.box(7, 0, 6, 15, 0, 16, p.light, p.light)
		p.box(6, 0, 6, 6, 3, 20, p.light, p.light)
		p.box(16, 0, 6, 16, 3, 20, p.light, p.light)
		p.box(7, 1, 7, 7, 1, 20, p.light, p.light)
		p.box(15, 1, 7, 15, 1, 20, p.light, p.light)
		p.box(7, 1, 6, 9, 3, 6, p.light, p.light)
		p.box(13, 1, 6, 15, 3, 6, p.light, p.light)
		p.box(8, 1, 7, 9, 1, 7, p.light, p.light)
		p.box(13, 1, 7, 14, 1, 7, p.light, p.light)
		p.box(9, 0, 5, 13, 0, 5, p.light, p.light)
		p.box(10, 0, 7, 12, 0, 7, p.black, p.black)
		p.box(8, 0, 10, 8, 0, 12, p.black, p.black)
		p.box(14, 0, 10, 14, 0, 12, p.black, p.black)
		for z := int32(18); z >= 7; z -= 3 {
			p.put(6, 3, z, p.lamp)
			p.put(16, 3, z, p.lamp)
		}
		p.put(10, 0, 10, p.lamp)
		p.put(12, 0, 10, p.lamp)
		p.put(10, 0, 12, p.lamp)
		p.put(12, 0, 12, p.lamp)
		p.put(8, 3, 6, p.lamp)
		p.put(14, 3, 6, p.lamp)
		p.put(4, 2, 4, p.light)
		p.put(4, 1, 4, p.lamp)
		p.put(4, 0, 4, p.light)
		p.put(18, 2, 4, p.light)
		p.put(18, 1, 4, p.lamp)
		p.put(18, 0, 4, p.light)
		p.put(4, 2, 18, p.light)
		p.put(4, 1, 18, p.lamp)
		p.put(4, 0, 18, p.light)
		p.put(18, 2, 18, p.light)
		p.put(18, 1, 18, p.lamp)
		p.put(18, 0, 18, p.light)
		p.put(9, 7, 20, p.light)
		p.put(13, 7, 20, p.light)
		p.box(6, 0, 21, 7, 4, 21, p.light, p.light)
		p.box(15, 0, 21, 16, 4, 21, p.light, p.light)
		p.spawnElder(11, 2, 16)
	case 1:
		p.box(9, 3, 18, 13, 3, 20, p.light, p.light)
		p.box(9, 0, 18, 9, 2, 18, p.light, p.light)
		p.box(13, 0, 18, 13, 2, 18, p.light, p.light)
		x := int32(9)
		for range 2 {
			p.put(x, 6, 20, p.light)
			p.put(x, 5, 20, p.lamp)
			p.put(x, 4, 20, p.light)
			x = 13
		}
		p.box(7, 3, 7, 15, 3, 14, p.light, p.light)
		x = 10
		for range 2 {
			p.box(x, 0, 10, x, 6, 10, p.light, p.light)
			p.box(x, 0, 12, x, 6, 12, p.light, p.light)
			p.put(x, 0, 10, p.lamp)
			p.put(x, 0, 12, p.lamp)
			p.put(x, 4, 10, p.lamp)
			p.put(x, 4, 12, p.lamp)
			x = 12
		}
		x = 8
		for range 2 {
			p.box(x, 0, 7, x, 2, 7, p.light, p.light)
			p.box(x, 0, 14, x, 2, 14, p.light, p.light)
			x = 14
		}
		p.box(8, 3, 8, 8, 3, 13, p.black, p.black)
		p.box(14, 3, 8, 14, 3, 13, p.black, p.black)
		p.spawnElder(11, 5, 13)
	}
}
