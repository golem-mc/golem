package world

//go:generate go run ../../../tools/gen ruinedportal-golden

import (
	"github.com/golem-mc/golem/internal/server/world/terrain"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

func init() {
	for _, id := range []string{"ruined_portal", "ruined_portal_desert", "ruined_portal_jungle", "ruined_portal_mountain", "ruined_portal_ocean", "ruined_portal_swamp"} {
		overworldStructureTypes["minecraft:"+id] = ruinedPortalStructure{"minecraft:" + id}
	}
}

type ruinedPortalStructure struct{ id string }

// vanilla: RuinedPortalStructure.findGenerationPoint
func (t ruinedPortalStructure) generationPoint(ctx *structureContext) ([3]int32, func() []structurePiece, bool) {
	setups := ruinedPortals().setups[t.id]
	setup := setups[0]
	if len(setups) > 1 {
		var total float32
		for _, s := range setups {
			total += s.Weight
		}
		pick := ctx.rng.NextFloat()
		for _, s := range setups {
			pick -= s.Weight / total
			if pick < 0 {
				setup = s
				break
			}
		}
	}
	p := newRuinedPortalPiece(ctx.rng, ctx.chunkX, ctx.chunkZ, setup)
	b := p.box
	center := [2]int32{b.minX + (b.maxX-b.minX+1)/2, b.minZ + (b.maxZ-b.minZ+1)/2}
	columns := ctx.g.portalColumns([5][2]int32{center, {b.minX, b.minZ}, {b.maxX, b.minZ}, {b.minX, b.maxZ}, {b.maxX, b.maxZ}})
	opaque := func(b terrain.Material) bool {
		if setup.Placement == "on_ocean_floor" {
			return b != terrain.Air && b != terrain.Water && b != terrain.Lava
		}
		return b != terrain.Air
	}
	y := int32(worldMinY)
	for i := len(columns[0]) - 1; i >= 0; i-- {
		if opaque(columns[0][i]) {
			y += int32(i)
			break
		}
	}
	const minY = worldMinY + 15
	interval := func(low, high int32) int32 {
		if low < high {
			return low + ctx.rng.NextIntN(high-low+1)
		}
		return high
	}
	// vanilla: RuinedPortalStructure.findSuitableY
	switch setup.Placement {
	case "in_mountain":
		y = interval(70, y-(b.maxY-b.minY+1))
	case "underground":
		y = interval(minY, y-(b.maxY-b.minY+1))
	case "partly_buried":
		y = y - (b.maxY - b.minY + 1) + 2 + ctx.rng.NextIntN(7)
	}
	for ; y > minY; y-- {
		solid := 0
		for _, column := range columns[1:] {
			if opaque(column[y-worldMinY]) {
				solid++
			}
		}
		if solid >= 3 {
			break
		}
	}
	p.pos = [3]int32{ctx.chunkX * 16, y, ctx.chunkZ * 16}
	p.box = b.moved(0, y, 0)
	return p.pos, func() []structurePiece {
		if setup.Cold {
			biome := ctx.g.biomeSource.At(p.pos[0]>>2, y>>2, p.pos[2]>>2)
			p.cold = ctx.g.temperatureAt(biome, p.pos[0], y, p.pos[2]) < rainTemperatureFloor
		}
		return []structurePiece{overworldPortalPiece{p}}
	}, true
}

// portalColumns reads the noise terrain without a beardifier, carvers or surface.
// vanilla: NoiseBasedChunkGenerator.getBaseColumn
func (g *OverworldGenerator) portalColumns(points [5][2]int32) [5][worldHeight]terrain.Material {
	var columns [5][worldHeight]terrain.Material
	if g.flat != nil {
		// vanilla: FlatLevelSource.getBaseColumn
		var col [worldHeight]terrain.Material
		for y := range col {
			switch s := g.flat.baseLayerAt(int32(y) + worldMinY); {
			case overworldAirBlocks[registry.BlockOf(s)]:
				col[y] = terrain.Air
			case registry.BlockOf(s) == registry.BlockWater:
				col[y] = terrain.Water
			case registry.BlockOf(s) == registry.BlockLava:
				col[y] = terrain.Lava
			default:
				col[y] = terrain.Stone
			}
		}
		for i := range columns {
			columns[i] = col
		}
		return columns
	}
	for i, point := range points {
		col := g.state.BaseColumn(point[0], point[1], worldMinY, worldHeight)
		copy(columns[i][:], col.Blocks)
	}
	return columns
}

type overworldPortalPiece struct{ *ruinedPortal }

func (p overworldPortalPiece) bounds() jbox      { return p.box }
func (p overworldPortalPiece) nbt() nbt.Compound { return p.pieceNBT() }

// vanilla: RuinedPortalPiece.postProcess
func (p overworldPortalPiece) place(r *decorRegion, chunk jbox, _ [3]int32) {
	b := p.box
	if chunk.inside(b.minX+(b.maxX-b.minX+1)/2, b.minY+(b.maxY-b.minY+1)/2, b.minZ+(b.maxZ-b.minZ+1)/2) {
		r.placeRuinedPortal(p.ruinedPortal)
	}
}
