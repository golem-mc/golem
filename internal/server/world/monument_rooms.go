package world

import (
	"github.com/golem-mc/golem/internal/server/world/levelgen"
	"github.com/golem-mc/golem/registry"
)

// room keeps random draws for sponge columns even when they fall outside the chunk.
func (p *monumentPlacer) room(piece levelgen.MonumentPiece) {
	switch piece.Kind {
	case levelgen.MonumentEntry:
		p.roomEntry(piece)
	case levelgen.MonumentCore:
		p.roomCore()
	case levelgen.MonumentDoubleXY:
		p.roomDoubleXY(piece)
	case levelgen.MonumentDoubleYZ:
		p.roomDoubleYZ(piece)
	case levelgen.MonumentDoubleZ:
		p.roomDoubleZ(piece)
	case levelgen.MonumentDoubleX:
		p.roomDoubleX(piece)
	case levelgen.MonumentDoubleY:
		p.roomDoubleY(piece)
	case levelgen.MonumentSimpleTop:
		p.roomSimpleTop(piece)
	case levelgen.MonumentSimple:
		p.roomSimple(piece)
	case levelgen.MonumentWing:
		p.wingRoom(piece.Design)
	case levelgen.MonumentPenthouse:
		p.penthouse()
	default:
		panic("unsupported monument room")
	}
}

// vanilla: OceanMonumentPieces.OceanMonumentEntryRoom.postProcess
func (p *monumentPlacer) roomEntry(piece levelgen.MonumentPiece) {
	room := piece.Room

	p.box(0, 3, 0, 2, 3, 7, p.light, p.light)
	p.box(5, 3, 0, 7, 3, 7, p.light, p.light)
	p.box(0, 2, 0, 1, 2, 7, p.light, p.light)
	p.box(6, 2, 0, 7, 2, 7, p.light, p.light)
	p.box(0, 1, 0, 0, 1, 7, p.light, p.light)
	p.box(7, 1, 0, 7, 1, 7, p.light, p.light)
	p.box(0, 1, 7, 7, 3, 7, p.light, p.light)
	p.box(1, 1, 0, 2, 3, 0, p.light, p.light)
	p.box(5, 1, 0, 6, 3, 0, p.light, p.light)
	if room.Openings[levelgen.MonumentNorth] {
		p.waterBox(3, 1, 7, 4, 2, 7)
	}
	if room.Openings[levelgen.MonumentWest] {
		p.waterBox(0, 1, 3, 1, 2, 4)
	}
	if room.Openings[levelgen.MonumentEast] {
		p.waterBox(6, 1, 3, 7, 2, 4)
	}
}

// vanilla: OceanMonumentPieces.OceanMonumentCoreRoom.postProcess
func (p *monumentPlacer) roomCore() {
	p.boxOnWater(1, 8, 0, 14, 8, 14, p.gray)
	block := p.light
	p.box(0, 7, 0, 0, 7, 15, block, block)
	p.box(15, 7, 0, 15, 7, 15, block, block)
	p.box(1, 7, 0, 15, 7, 0, block, block)
	p.box(1, 7, 15, 14, 7, 15, block, block)
	for y := int32(1); y <= 6; y++ {
		block := p.light
		if y == 2 || y == 6 {
			block = p.gray
		}
		for x := int32(0); x <= 15; x += 15 {
			p.box(x, y, 0, x, y, 1, block, block)
			p.box(x, y, 6, x, y, 9, block, block)
			p.box(x, y, 14, x, y, 15, block, block)
		}
		p.box(1, y, 0, 1, y, 0, block, block)
		p.box(6, y, 0, 9, y, 0, block, block)
		p.box(14, y, 0, 14, y, 0, block, block)
		p.box(1, y, 15, 14, y, 15, block, block)
	}
	p.box(6, 3, 6, 9, 6, 9, p.black, p.black)
	p.box(7, 4, 7, 8, 5, 8, registry.BlockGoldBlock.DefaultState(), registry.BlockGoldBlock.DefaultState())
	for y := int32(3); y <= 6; y += 3 {
		for x := int32(6); x <= 9; x += 3 {
			p.put(x, y, 6, p.lamp)
			p.put(x, y, 9, p.lamp)
		}
	}
	p.box(5, 1, 6, 5, 2, 6, p.light, p.light)
	p.box(5, 1, 9, 5, 2, 9, p.light, p.light)
	p.box(10, 1, 6, 10, 2, 6, p.light, p.light)
	p.box(10, 1, 9, 10, 2, 9, p.light, p.light)
	p.box(6, 1, 5, 6, 2, 5, p.light, p.light)
	p.box(9, 1, 5, 9, 2, 5, p.light, p.light)
	p.box(6, 1, 10, 6, 2, 10, p.light, p.light)
	p.box(9, 1, 10, 9, 2, 10, p.light, p.light)
	p.box(5, 2, 5, 5, 6, 5, p.light, p.light)
	p.box(5, 2, 10, 5, 6, 10, p.light, p.light)
	p.box(10, 2, 5, 10, 6, 5, p.light, p.light)
	p.box(10, 2, 10, 10, 6, 10, p.light, p.light)
	p.box(5, 7, 1, 5, 7, 6, p.light, p.light)
	p.box(10, 7, 1, 10, 7, 6, p.light, p.light)
	p.box(5, 7, 9, 5, 7, 14, p.light, p.light)
	p.box(10, 7, 9, 10, 7, 14, p.light, p.light)
	p.box(1, 7, 5, 6, 7, 5, p.light, p.light)
	p.box(1, 7, 10, 6, 7, 10, p.light, p.light)
	p.box(9, 7, 5, 14, 7, 5, p.light, p.light)
	p.box(9, 7, 10, 14, 7, 10, p.light, p.light)
	p.box(2, 1, 2, 2, 1, 3, p.light, p.light)
	p.box(3, 1, 2, 3, 1, 2, p.light, p.light)
	p.box(13, 1, 2, 13, 1, 3, p.light, p.light)
	p.box(12, 1, 2, 12, 1, 2, p.light, p.light)
	p.box(2, 1, 12, 2, 1, 13, p.light, p.light)
	p.box(3, 1, 13, 3, 1, 13, p.light, p.light)
	p.box(13, 1, 12, 13, 1, 13, p.light, p.light)
	p.box(12, 1, 13, 12, 1, 13, p.light, p.light)
}

// vanilla: OceanMonumentPieces.OceanMonumentDoubleXYRoom.postProcess
func (p *monumentPlacer) roomDoubleXY(piece levelgen.MonumentPiece) {
	room := piece.Room

	east := room.Connections[levelgen.MonumentEast]
	west := room
	westUp := west.Connections[levelgen.MonumentUp]
	eastUp := east.Connections[levelgen.MonumentUp]
	if room.Index/25 > 0 {
		p.roomFloor(8, 0, east.Openings[levelgen.MonumentDown])
		p.roomFloor(0, 0, west.Openings[levelgen.MonumentDown])
	}
	if westUp.Connections[levelgen.MonumentUp] == nil {
		p.boxOnWater(1, 8, 1, 7, 8, 6, p.gray)
	}
	if eastUp.Connections[levelgen.MonumentUp] == nil {
		p.boxOnWater(8, 8, 1, 14, 8, 6, p.gray)
	}
	for y := int32(1); y <= 7; y++ {
		block := p.light
		if y == 2 || y == 6 {
			block = p.gray
		}
		p.box(0, y, 0, 0, y, 7, block, block)
		p.box(15, y, 0, 15, y, 7, block, block)
		p.box(1, y, 0, 15, y, 0, block, block)
		p.box(1, y, 7, 14, y, 7, block, block)
	}
	p.box(2, 1, 3, 2, 7, 4, p.light, p.light)
	p.box(3, 1, 2, 4, 7, 2, p.light, p.light)
	p.box(3, 1, 5, 4, 7, 5, p.light, p.light)
	p.box(13, 1, 3, 13, 7, 4, p.light, p.light)
	p.box(11, 1, 2, 12, 7, 2, p.light, p.light)
	p.box(11, 1, 5, 12, 7, 5, p.light, p.light)
	p.box(5, 1, 3, 5, 3, 4, p.light, p.light)
	p.box(10, 1, 3, 10, 3, 4, p.light, p.light)
	p.box(5, 7, 2, 10, 7, 5, p.light, p.light)
	p.box(5, 5, 2, 5, 7, 2, p.light, p.light)
	p.box(10, 5, 2, 10, 7, 2, p.light, p.light)
	p.box(5, 5, 5, 5, 7, 5, p.light, p.light)
	p.box(10, 5, 5, 10, 7, 5, p.light, p.light)
	p.put(6, 6, 2, p.light)
	p.put(9, 6, 2, p.light)
	p.put(6, 6, 5, p.light)
	p.put(9, 6, 5, p.light)
	p.box(5, 4, 3, 6, 4, 4, p.light, p.light)
	p.box(9, 4, 3, 10, 4, 4, p.light, p.light)
	p.put(5, 4, 2, p.lamp)
	p.put(5, 4, 5, p.lamp)
	p.put(10, 4, 2, p.lamp)
	p.put(10, 4, 5, p.lamp)
	p.roomDoors(west, 0, 1, 0, levelgen.MonumentSouth, levelgen.MonumentNorth, levelgen.MonumentWest)
	p.roomDoors(east, 8, 1, 0, levelgen.MonumentSouth, levelgen.MonumentNorth, levelgen.MonumentEast)
	p.roomDoors(westUp, 0, 5, 0, levelgen.MonumentSouth, levelgen.MonumentNorth, levelgen.MonumentWest)
	p.roomDoors(eastUp, 8, 5, 0, levelgen.MonumentSouth, levelgen.MonumentNorth, levelgen.MonumentEast)
}

// vanilla: OceanMonumentPieces.OceanMonumentDoubleYZRoom.postProcess
func (p *monumentPlacer) roomDoubleYZ(piece levelgen.MonumentPiece) {
	room := piece.Room

	north := room.Connections[levelgen.MonumentNorth]
	south := room
	northUp := north.Connections[levelgen.MonumentUp]
	southUp := south.Connections[levelgen.MonumentUp]
	if room.Index/25 > 0 {
		p.roomFloor(0, 8, north.Openings[levelgen.MonumentDown])
		p.roomFloor(0, 0, south.Openings[levelgen.MonumentDown])
	}
	if southUp.Connections[levelgen.MonumentUp] == nil {
		p.boxOnWater(1, 8, 1, 6, 8, 7, p.gray)
	}
	if northUp.Connections[levelgen.MonumentUp] == nil {
		p.boxOnWater(1, 8, 8, 6, 8, 14, p.gray)
	}
	for y := int32(1); y <= 7; y++ {
		block := p.light
		if y == 2 || y == 6 {
			block = p.gray
		}
		p.box(0, y, 0, 0, y, 15, block, block)
		p.box(7, y, 0, 7, y, 15, block, block)
		p.box(1, y, 0, 6, y, 0, block, block)
		p.box(1, y, 15, 6, y, 15, block, block)
	}
	for y := int32(1); y <= 7; y++ {
		block := p.black
		if y == 2 || y == 6 {
			block = p.lamp
		}
		p.box(3, y, 7, 4, y, 8, block, block)
	}
	p.roomDoors(south, 0, 1, 0, levelgen.MonumentSouth, levelgen.MonumentEast, levelgen.MonumentWest)
	p.roomDoors(north, 0, 1, 8, levelgen.MonumentNorth, levelgen.MonumentWest, levelgen.MonumentEast)
	if southUp.Openings[levelgen.MonumentSouth] {
		p.waterBox(3, 5, 0, 4, 6, 0)
	}
	if southUp.Openings[levelgen.MonumentEast] {
		p.waterBox(7, 5, 3, 7, 6, 4)
		p.box(5, 4, 2, 6, 4, 5, p.light, p.light)
		p.box(6, 1, 2, 6, 3, 2, p.light, p.light)
		p.box(6, 1, 5, 6, 3, 5, p.light, p.light)
	}
	if southUp.Openings[levelgen.MonumentWest] {
		p.waterBox(0, 5, 3, 0, 6, 4)
		p.box(1, 4, 2, 2, 4, 5, p.light, p.light)
		p.box(1, 1, 2, 1, 3, 2, p.light, p.light)
		p.box(1, 1, 5, 1, 3, 5, p.light, p.light)
	}
	if northUp.Openings[levelgen.MonumentNorth] {
		p.waterBox(3, 5, 15, 4, 6, 15)
	}
	if northUp.Openings[levelgen.MonumentWest] {
		p.waterBox(0, 5, 11, 0, 6, 12)
		p.box(1, 4, 10, 2, 4, 13, p.light, p.light)
		p.box(1, 1, 10, 1, 3, 10, p.light, p.light)
		p.box(1, 1, 13, 1, 3, 13, p.light, p.light)
	}
	if northUp.Openings[levelgen.MonumentEast] {
		p.waterBox(7, 5, 11, 7, 6, 12)
		p.box(5, 4, 10, 6, 4, 13, p.light, p.light)
		p.box(6, 1, 10, 6, 3, 10, p.light, p.light)
		p.box(6, 1, 13, 6, 3, 13, p.light, p.light)
	}
}

// vanilla: OceanMonumentPieces.OceanMonumentDoubleZRoom.postProcess
func (p *monumentPlacer) roomDoubleZ(piece levelgen.MonumentPiece) {
	room := piece.Room

	north := room.Connections[levelgen.MonumentNorth]
	south := room
	if room.Index/25 > 0 {
		p.roomFloor(0, 8, north.Openings[levelgen.MonumentDown])
		p.roomFloor(0, 0, south.Openings[levelgen.MonumentDown])
	}
	if south.Connections[levelgen.MonumentUp] == nil {
		p.boxOnWater(1, 4, 1, 6, 4, 7, p.gray)
	}
	if north.Connections[levelgen.MonumentUp] == nil {
		p.boxOnWater(1, 4, 8, 6, 4, 14, p.gray)
	}
	p.box(0, 3, 0, 0, 3, 15, p.light, p.light)
	p.box(7, 3, 0, 7, 3, 15, p.light, p.light)
	p.box(1, 3, 0, 7, 3, 0, p.light, p.light)
	p.box(1, 3, 15, 6, 3, 15, p.light, p.light)
	p.box(0, 2, 0, 0, 2, 15, p.gray, p.gray)
	p.box(7, 2, 0, 7, 2, 15, p.gray, p.gray)
	p.box(1, 2, 0, 7, 2, 0, p.gray, p.gray)
	p.box(1, 2, 15, 6, 2, 15, p.gray, p.gray)
	p.box(0, 1, 0, 0, 1, 15, p.light, p.light)
	p.box(7, 1, 0, 7, 1, 15, p.light, p.light)
	p.box(1, 1, 0, 7, 1, 0, p.light, p.light)
	p.box(1, 1, 15, 6, 1, 15, p.light, p.light)
	p.box(1, 1, 1, 1, 1, 2, p.light, p.light)
	p.box(6, 1, 1, 6, 1, 2, p.light, p.light)
	p.box(1, 3, 1, 1, 3, 2, p.light, p.light)
	p.box(6, 3, 1, 6, 3, 2, p.light, p.light)
	p.box(1, 1, 13, 1, 1, 14, p.light, p.light)
	p.box(6, 1, 13, 6, 1, 14, p.light, p.light)
	p.box(1, 3, 13, 1, 3, 14, p.light, p.light)
	p.box(6, 3, 13, 6, 3, 14, p.light, p.light)
	p.box(2, 1, 6, 2, 3, 6, p.light, p.light)
	p.box(5, 1, 6, 5, 3, 6, p.light, p.light)
	p.box(2, 1, 9, 2, 3, 9, p.light, p.light)
	p.box(5, 1, 9, 5, 3, 9, p.light, p.light)
	p.box(3, 2, 6, 4, 2, 6, p.light, p.light)
	p.box(3, 2, 9, 4, 2, 9, p.light, p.light)
	p.box(2, 2, 7, 2, 2, 8, p.light, p.light)
	p.box(5, 2, 7, 5, 2, 8, p.light, p.light)
	p.put(2, 2, 5, p.lamp)
	p.put(5, 2, 5, p.lamp)
	p.put(2, 2, 10, p.lamp)
	p.put(5, 2, 10, p.lamp)
	p.put(2, 3, 5, p.light)
	p.put(5, 3, 5, p.light)
	p.put(2, 3, 10, p.light)
	p.put(5, 3, 10, p.light)
	p.roomDoors(south, 0, 1, 0, levelgen.MonumentSouth, levelgen.MonumentEast, levelgen.MonumentWest)
	p.roomDoors(north, 0, 1, 8, levelgen.MonumentNorth, levelgen.MonumentWest, levelgen.MonumentEast)
}

// vanilla: OceanMonumentPieces.OceanMonumentDoubleXRoom.postProcess
func (p *monumentPlacer) roomDoubleX(piece levelgen.MonumentPiece) {
	room := piece.Room

	east := room.Connections[levelgen.MonumentEast]
	west := room
	if room.Index/25 > 0 {
		p.roomFloor(8, 0, east.Openings[levelgen.MonumentDown])
		p.roomFloor(0, 0, west.Openings[levelgen.MonumentDown])
	}
	if west.Connections[levelgen.MonumentUp] == nil {
		p.boxOnWater(1, 4, 1, 7, 4, 6, p.gray)
	}
	if east.Connections[levelgen.MonumentUp] == nil {
		p.boxOnWater(8, 4, 1, 14, 4, 6, p.gray)
	}
	p.box(0, 3, 0, 0, 3, 7, p.light, p.light)
	p.box(15, 3, 0, 15, 3, 7, p.light, p.light)
	p.box(1, 3, 0, 15, 3, 0, p.light, p.light)
	p.box(1, 3, 7, 14, 3, 7, p.light, p.light)
	p.box(0, 2, 0, 0, 2, 7, p.gray, p.gray)
	p.box(15, 2, 0, 15, 2, 7, p.gray, p.gray)
	p.box(1, 2, 0, 15, 2, 0, p.gray, p.gray)
	p.box(1, 2, 7, 14, 2, 7, p.gray, p.gray)
	p.box(0, 1, 0, 0, 1, 7, p.light, p.light)
	p.box(15, 1, 0, 15, 1, 7, p.light, p.light)
	p.box(1, 1, 0, 15, 1, 0, p.light, p.light)
	p.box(1, 1, 7, 14, 1, 7, p.light, p.light)
	p.box(5, 1, 0, 10, 1, 4, p.light, p.light)
	p.box(6, 2, 0, 9, 2, 3, p.gray, p.gray)
	p.box(5, 3, 0, 10, 3, 4, p.light, p.light)
	p.put(6, 2, 3, p.lamp)
	p.put(9, 2, 3, p.lamp)
	p.roomDoors(west, 0, 1, 0, levelgen.MonumentSouth, levelgen.MonumentNorth, levelgen.MonumentWest)
	p.roomDoors(east, 8, 1, 0, levelgen.MonumentSouth, levelgen.MonumentNorth, levelgen.MonumentEast)
}

// vanilla: OceanMonumentPieces.OceanMonumentDoubleYRoom.postProcess
func (p *monumentPlacer) roomDoubleY(piece levelgen.MonumentPiece) {
	room := piece.Room

	if room.Index/25 > 0 {
		p.roomFloor(0, 0, room.Openings[levelgen.MonumentDown])
	}
	above := room.Connections[levelgen.MonumentUp]
	if above.Connections[levelgen.MonumentUp] == nil {
		p.boxOnWater(1, 8, 1, 6, 8, 6, p.gray)
	}
	p.box(0, 4, 0, 0, 4, 7, p.light, p.light)
	p.box(7, 4, 0, 7, 4, 7, p.light, p.light)
	p.box(1, 4, 0, 6, 4, 0, p.light, p.light)
	p.box(1, 4, 7, 6, 4, 7, p.light, p.light)
	p.box(2, 4, 1, 2, 4, 2, p.light, p.light)
	p.box(1, 4, 2, 1, 4, 2, p.light, p.light)
	p.box(5, 4, 1, 5, 4, 2, p.light, p.light)
	p.box(6, 4, 2, 6, 4, 2, p.light, p.light)
	p.box(2, 4, 5, 2, 4, 6, p.light, p.light)
	p.box(1, 4, 5, 1, 4, 5, p.light, p.light)
	p.box(5, 4, 5, 5, 4, 6, p.light, p.light)
	p.box(6, 4, 5, 6, 4, 5, p.light, p.light)
	definition := room
	for y := int32(1); y <= 5; y += 4 {
		z := int32(0)
		if definition.Openings[levelgen.MonumentSouth] {
			p.box(2, y, z, 2, y+2, z, p.light, p.light)
			p.box(5, y, z, 5, y+2, z, p.light, p.light)
			p.box(3, y+2, z, 4, y+2, z, p.light, p.light)
		} else {
			p.box(0, y, z, 7, y+2, z, p.light, p.light)
			p.box(0, y+1, z, 7, y+1, z, p.gray, p.gray)
		}
		z = 7
		if definition.Openings[levelgen.MonumentNorth] {
			p.box(2, y, z, 2, y+2, z, p.light, p.light)
			p.box(5, y, z, 5, y+2, z, p.light, p.light)
			p.box(3, y+2, z, 4, y+2, z, p.light, p.light)
		} else {
			p.box(0, y, z, 7, y+2, z, p.light, p.light)
			p.box(0, y+1, z, 7, y+1, z, p.gray, p.gray)
		}
		x := int32(0)
		if definition.Openings[levelgen.MonumentWest] {
			p.box(x, y, 2, x, y+2, 2, p.light, p.light)
			p.box(x, y, 5, x, y+2, 5, p.light, p.light)
			p.box(x, y+2, 3, x, y+2, 4, p.light, p.light)
		} else {
			p.box(x, y, 0, x, y+2, 7, p.light, p.light)
			p.box(x, y+1, 0, x, y+1, 7, p.gray, p.gray)
		}
		x = 7
		if definition.Openings[levelgen.MonumentEast] {
			p.box(x, y, 2, x, y+2, 2, p.light, p.light)
			p.box(x, y, 5, x, y+2, 5, p.light, p.light)
			p.box(x, y+2, 3, x, y+2, 4, p.light, p.light)
		} else {
			p.box(x, y, 0, x, y+2, 7, p.light, p.light)
			p.box(x, y+1, 0, x, y+1, 7, p.gray, p.gray)
		}
		definition = above
	}
}

// vanilla: OceanMonumentPieces.OceanMonumentSimpleTopRoom.postProcess
func (p *monumentPlacer) roomSimpleTop(piece levelgen.MonumentPiece) {
	room := piece.Room

	if room.Index/25 > 0 {
		p.roomFloor(0, 0, room.Openings[levelgen.MonumentDown])
	}
	if room.Connections[levelgen.MonumentUp] == nil {
		p.boxOnWater(1, 4, 1, 6, 4, 6, p.gray)
	}
	for x := int32(1); x <= 6; x++ {
		for z := int32(1); z <= 6; z++ {
			if p.rng.NextIntN(3) == 0 {
				continue
			}
			y0 := int32(3)
			if p.rng.NextIntN(4) == 0 {
				y0 = 2
			}
			wetSponge := registry.BlockWetSponge.DefaultState()
			p.box(x, y0, z, x, 3, z, wetSponge, wetSponge)
		}
	}
	p.box(0, 1, 0, 0, 1, 7, p.light, p.light)
	p.box(7, 1, 0, 7, 1, 7, p.light, p.light)
	p.box(1, 1, 0, 6, 1, 0, p.light, p.light)
	p.box(1, 1, 7, 6, 1, 7, p.light, p.light)
	p.box(0, 2, 0, 0, 2, 7, p.black, p.black)
	p.box(7, 2, 0, 7, 2, 7, p.black, p.black)
	p.box(1, 2, 0, 6, 2, 0, p.black, p.black)
	p.box(1, 2, 7, 6, 2, 7, p.black, p.black)
	p.box(0, 3, 0, 0, 3, 7, p.light, p.light)
	p.box(7, 3, 0, 7, 3, 7, p.light, p.light)
	p.box(1, 3, 0, 6, 3, 0, p.light, p.light)
	p.box(1, 3, 7, 6, 3, 7, p.light, p.light)
	p.box(0, 1, 3, 0, 2, 4, p.black, p.black)
	p.box(7, 1, 3, 7, 2, 4, p.black, p.black)
	p.box(3, 1, 0, 4, 2, 0, p.black, p.black)
	p.box(3, 1, 7, 4, 2, 7, p.black, p.black)
	if room.Openings[levelgen.MonumentSouth] {
		p.waterBox(3, 1, 0, 4, 2, 0)
	}
}

// vanilla: OceanMonumentPieces.OceanMonumentSimpleRoom.postProcess
func (p *monumentPlacer) roomSimple(piece levelgen.MonumentPiece) {
	room := piece.Room

	if room.Index/25 > 0 {
		p.roomFloor(0, 0, room.Openings[levelgen.MonumentDown])
	}
	if room.Connections[levelgen.MonumentUp] == nil {
		p.boxOnWater(1, 4, 1, 6, 4, 6, p.gray)
	}
	centerPillar := piece.Design != 0 && p.rng.NextBoolean() && !room.Openings[levelgen.MonumentDown] && !room.Openings[levelgen.MonumentUp] && monumentOpeningCount(room) > 1
	switch piece.Design {
	case 0:
		p.box(0, 1, 0, 2, 1, 2, p.light, p.light)
		p.box(0, 3, 0, 2, 3, 2, p.light, p.light)
		p.box(0, 2, 0, 0, 2, 2, p.gray, p.gray)
		p.box(1, 2, 0, 2, 2, 0, p.gray, p.gray)
		p.put(1, 2, 1, p.lamp)
		p.box(5, 1, 0, 7, 1, 2, p.light, p.light)
		p.box(5, 3, 0, 7, 3, 2, p.light, p.light)
		p.box(7, 2, 0, 7, 2, 2, p.gray, p.gray)
		p.box(5, 2, 0, 6, 2, 0, p.gray, p.gray)
		p.put(6, 2, 1, p.lamp)
		p.box(0, 1, 5, 2, 1, 7, p.light, p.light)
		p.box(0, 3, 5, 2, 3, 7, p.light, p.light)
		p.box(0, 2, 5, 0, 2, 7, p.gray, p.gray)
		p.box(1, 2, 7, 2, 2, 7, p.gray, p.gray)
		p.put(1, 2, 6, p.lamp)
		p.box(5, 1, 5, 7, 1, 7, p.light, p.light)
		p.box(5, 3, 5, 7, 3, 7, p.light, p.light)
		p.box(7, 2, 5, 7, 2, 7, p.gray, p.gray)
		p.box(5, 2, 7, 6, 2, 7, p.gray, p.gray)
		p.put(6, 2, 6, p.lamp)
		if room.Openings[levelgen.MonumentSouth] {
			p.box(3, 3, 0, 4, 3, 0, p.light, p.light)
		} else {
			p.box(3, 3, 0, 4, 3, 1, p.light, p.light)
			p.box(3, 2, 0, 4, 2, 0, p.gray, p.gray)
			p.box(3, 1, 0, 4, 1, 1, p.light, p.light)
		}
		if room.Openings[levelgen.MonumentNorth] {
			p.box(3, 3, 7, 4, 3, 7, p.light, p.light)
		} else {
			p.box(3, 3, 6, 4, 3, 7, p.light, p.light)
			p.box(3, 2, 7, 4, 2, 7, p.gray, p.gray)
			p.box(3, 1, 6, 4, 1, 7, p.light, p.light)
		}
		if room.Openings[levelgen.MonumentWest] {
			p.box(0, 3, 3, 0, 3, 4, p.light, p.light)
		} else {
			p.box(0, 3, 3, 1, 3, 4, p.light, p.light)
			p.box(0, 2, 3, 0, 2, 4, p.gray, p.gray)
			p.box(0, 1, 3, 1, 1, 4, p.light, p.light)
		}
		if room.Openings[levelgen.MonumentEast] {
			p.box(7, 3, 3, 7, 3, 4, p.light, p.light)
		} else {
			p.box(6, 3, 3, 7, 3, 4, p.light, p.light)
			p.box(7, 2, 3, 7, 2, 4, p.gray, p.gray)
			p.box(6, 1, 3, 7, 1, 4, p.light, p.light)
		}
	case 1:
		p.box(2, 1, 2, 2, 3, 2, p.light, p.light)
		p.box(2, 1, 5, 2, 3, 5, p.light, p.light)
		p.box(5, 1, 5, 5, 3, 5, p.light, p.light)
		p.box(5, 1, 2, 5, 3, 2, p.light, p.light)
		p.put(2, 2, 2, p.lamp)
		p.put(2, 2, 5, p.lamp)
		p.put(5, 2, 5, p.lamp)
		p.put(5, 2, 2, p.lamp)
		p.box(0, 1, 0, 1, 3, 0, p.light, p.light)
		p.box(0, 1, 1, 0, 3, 1, p.light, p.light)
		p.box(0, 1, 7, 1, 3, 7, p.light, p.light)
		p.box(0, 1, 6, 0, 3, 6, p.light, p.light)
		p.box(6, 1, 7, 7, 3, 7, p.light, p.light)
		p.box(7, 1, 6, 7, 3, 6, p.light, p.light)
		p.box(6, 1, 0, 7, 3, 0, p.light, p.light)
		p.box(7, 1, 1, 7, 3, 1, p.light, p.light)
		p.put(1, 2, 0, p.gray)
		p.put(0, 2, 1, p.gray)
		p.put(1, 2, 7, p.gray)
		p.put(0, 2, 6, p.gray)
		p.put(6, 2, 7, p.gray)
		p.put(7, 2, 6, p.gray)
		p.put(6, 2, 0, p.gray)
		p.put(7, 2, 1, p.gray)
		if !room.Openings[levelgen.MonumentSouth] {
			p.box(1, 3, 0, 6, 3, 0, p.light, p.light)
			p.box(1, 2, 0, 6, 2, 0, p.gray, p.gray)
			p.box(1, 1, 0, 6, 1, 0, p.light, p.light)
		}
		if !room.Openings[levelgen.MonumentNorth] {
			p.box(1, 3, 7, 6, 3, 7, p.light, p.light)
			p.box(1, 2, 7, 6, 2, 7, p.gray, p.gray)
			p.box(1, 1, 7, 6, 1, 7, p.light, p.light)
		}
		if !room.Openings[levelgen.MonumentWest] {
			p.box(0, 3, 1, 0, 3, 6, p.light, p.light)
			p.box(0, 2, 1, 0, 2, 6, p.gray, p.gray)
			p.box(0, 1, 1, 0, 1, 6, p.light, p.light)
		}
		if !room.Openings[levelgen.MonumentEast] {
			p.box(7, 3, 1, 7, 3, 6, p.light, p.light)
			p.box(7, 2, 1, 7, 2, 6, p.gray, p.gray)
			p.box(7, 1, 1, 7, 1, 6, p.light, p.light)
		}
	case 2:
		p.box(0, 1, 0, 0, 1, 7, p.light, p.light)
		p.box(7, 1, 0, 7, 1, 7, p.light, p.light)
		p.box(1, 1, 0, 6, 1, 0, p.light, p.light)
		p.box(1, 1, 7, 6, 1, 7, p.light, p.light)
		p.box(0, 2, 0, 0, 2, 7, p.black, p.black)
		p.box(7, 2, 0, 7, 2, 7, p.black, p.black)
		p.box(1, 2, 0, 6, 2, 0, p.black, p.black)
		p.box(1, 2, 7, 6, 2, 7, p.black, p.black)
		p.box(0, 3, 0, 0, 3, 7, p.light, p.light)
		p.box(7, 3, 0, 7, 3, 7, p.light, p.light)
		p.box(1, 3, 0, 6, 3, 0, p.light, p.light)
		p.box(1, 3, 7, 6, 3, 7, p.light, p.light)
		p.box(0, 1, 3, 0, 2, 4, p.black, p.black)
		p.box(7, 1, 3, 7, 2, 4, p.black, p.black)
		p.box(3, 1, 0, 4, 2, 0, p.black, p.black)
		p.box(3, 1, 7, 4, 2, 7, p.black, p.black)
		p.roomDoors(room, 0, 1, 0, levelgen.MonumentSouth, levelgen.MonumentNorth, levelgen.MonumentWest, levelgen.MonumentEast)
	}
	if centerPillar {
		p.box(3, 1, 3, 4, 1, 4, p.light, p.light)
		p.box(3, 2, 3, 4, 2, 4, p.gray, p.gray)
		p.box(3, 3, 3, 4, 3, 4, p.light, p.light)
	}
}
