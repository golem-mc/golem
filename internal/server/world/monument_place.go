package world

import (
	"github.com/golem-mc/golem/internal/server/world/levelgen"
	"github.com/golem-mc/golem/registry"
)

var (
	monumentFillKeep           = blockSet(registry.BlockIce, registry.BlockPackedIce, registry.BlockBlueIce, registry.BlockWater)
	structureColumnReplaceable = blockSet(registry.BlockWater, registry.BlockLava, registry.BlockGlowLichen, registry.BlockSeagrass, registry.BlockTallSeagrass)
)

// monumentPlacer shares the stronghold's chunk-clipped box, transform and
// fluid-scheduling primitives. Monument blocks have no directional properties.
type monumentPlacer struct {
	strongholdPlacer
	gray, light, black, lamp uint16
	seaLevel                 int32
}

func newMonumentPlacer(r *decorRegion, chunk jbox, frame levelgen.PieceFrame, seaLevel int32) *monumentPlacer {
	return &monumentPlacer{
		r: r, st: shStatesOnce(), frame: frame, chunk: chunk,
		gray: registry.BlockPrismarine.DefaultState(), light: registry.BlockPrismarineBricks.DefaultState(),
		black: registry.BlockDarkPrismarine.DefaultState(), lamp: registry.BlockSeaLantern.DefaultState(),
		seaLevel: seaLevel,
	}
}

// exterior stops before the fitted child rooms; the interior must run after
// the final water skirt. vanilla: MonumentBuilding.postProcess
func (p *monumentPlacer) exterior() {
	p.waterBox(0, 0, 0, 58, max(p.seaLevel, 64)-int32(p.frame.BB.MinY), 58)
	p.wing(false, 0)
	p.wing(true, 33)
	p.entranceArchs()
	p.entranceWall()
	p.roofPiece()
	p.lowerWall()
	p.middleWall()
	p.upperWall()
	for px := range int32(7) {
		for pz := int32(0); pz < 7; {
			if pz == 0 && px == 3 {
				pz = 6
			}
			for w := range int32(4) {
				for d := range int32(4) {
					x, z := px*9+w, pz*9+d
					p.put(x, 0, z, p.light)
					p.fillColumnDown(p.light, x, -1, z)
				}
			}
			if px == 0 || px == 6 {
				pz++
			} else {
				pz += 6
			}
		}
	}
	for i := range int32(5) {
		p.waterBox(-1-i, i*2, -1-i, -1-i, 23, 58+i)
		p.waterBox(58+i, i*2, -1-i, 58+i, 23, 58+i)
		p.waterBox(-i, i*2, -1-i, 57+i, 23, -1-i)
		p.waterBox(-i, i*2, 58+i, 57+i, 23, 58+i)
	}
}

// vanilla: OceanMonumentPiece.generateWaterBox
func (p *monumentPlacer) waterBox(x0, y0, z0, x1, y1, z1 int32) {
	for y := y0; y <= y1; y++ {
		for x := x0; x <= x1; x++ {
			for z := z0; z <= z1; z++ {
				wx, wy, wz := p.worldPos(x, y, z)
				if !p.chunk.inside(wx, wy, wz) {
					continue
				}
				state := p.r.get(wx, wy, wz)
				if monumentFillKeep[registry.BlockOf(state)] {
					continue
				}
				fill := p.st.water
				if wy >= p.seaLevel {
					fill = airState
				}
				p.put(x, y, z, fill)
			}
		}
	}
}

// vanilla: OceanMonumentPiece.chunkIntersects
func (p *monumentPlacer) intersects(x0, z0, x1, z1 int32) bool {
	x0, _, z0 = p.worldPos(x0, 0, z0)
	x1, _, z1 = p.worldPos(x1, 0, z1)
	return p.chunk.maxX >= min(x0, x1) && p.chunk.minX <= max(x0, x1) &&
		p.chunk.maxZ >= min(z0, z1) && p.chunk.minZ <= max(z0, z1)
}

// vanilla: OceanMonumentPiece.generateDefaultFloor
func (p *monumentPlacer) roomFloor(x, z int32, downOpening bool) {
	if !downOpening {
		p.box(x, 0, z, x+7, 0, z+7, p.gray, p.gray)
		return
	}
	p.box(x, 0, z, x+2, 0, z+7, p.gray, p.gray)
	p.box(x+5, 0, z, x+7, 0, z+7, p.gray, p.gray)
	p.box(x+3, 0, z, x+4, 0, z+2, p.gray, p.gray)
	p.box(x+3, 0, z+5, x+4, 0, z+7, p.gray, p.gray)
	p.box(x+3, 0, z+2, x+4, 0, z+2, p.light, p.light)
	p.box(x+3, 0, z+5, x+4, 0, z+5, p.light, p.light)
	p.box(x+2, 0, z+3, x+2, 0, z+4, p.light, p.light)
	p.box(x+5, 0, z+3, x+5, 0, z+4, p.light, p.light)
}

// boxOnWater replaces source water only. Flowing water and ice survive roofs.
// vanilla: OceanMonumentPiece.generateBoxOnFillOnly
func (p *monumentPlacer) boxOnWater(x0, y0, z0, x1, y1, z1 int32, state uint16) {
	for y := y0; y <= y1; y++ {
		for x := x0; x <= x1; x++ {
			for z := z0; z <= z1; z++ {
				wx, wy, wz := p.worldPos(x, y, z)
				if p.chunk.inside(wx, wy, wz) && p.r.get(wx, wy, wz) == p.st.water {
					p.put(x, y, z, state)
				}
			}
		}
	}
}

func monumentOpeningCount(room *levelgen.MonumentRoom) int {
	count := 0
	for _, open := range room.Openings {
		if open {
			count++
		}
	}
	return count
}

func (p *monumentPlacer) roomDoors(room *levelgen.MonumentRoom, x, y, z int32, directions ...int) {
	for _, d := range directions {
		if !room.Openings[d] {
			continue
		}
		switch d {
		case levelgen.MonumentSouth:
			p.waterBox(x+3, y, z, x+4, y+1, z)
		case levelgen.MonumentNorth:
			p.waterBox(x+3, y, z+7, x+4, y+1, z+7)
		case levelgen.MonumentWest:
			p.waterBox(x, y, z+3, x, y+1, z+4)
		case levelgen.MonumentEast:
			p.waterBox(x+7, y, z+3, x+7, y+1, z+4)
		}
	}
}
