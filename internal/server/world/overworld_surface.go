package world

import (
	"github.com/golem-mc/golem/internal/server/world/terrain"
	"sync"

	"github.com/golem-mc/golem/internal/server/world/levelgen"
	"github.com/golem-mc/golem/registry"
)

// cellCacheMax bounds the biome cell grids held under surfaceMu (1536 bytes each).
const cellCacheMax = 4096

// biomeCells is a chunk's grid and the set of its biomes, filled once.
// Never written after the fill, so readers share it.
type biomeCells struct {
	once sync.Once
	grid biomeGrid
	mask uint64
}

// biomeCellsAt is the chunk's biome cells, filled as vanilla fills a chunk:
// the source's search depends on the previous result, so a cell is only the
// same when its whole chunk is filled in order.
func (g *OverworldGenerator) biomeCellsAt(cx, cz int32) *biomeGrid {
	return &g.biomeCellsOf(cx, cz).grid
}

// biomeCellsOf finds or inserts the chunk's entry and fills it outside the
// lock, so concurrent callers of one chunk wait for a single fill.
func (g *OverworldGenerator) biomeCellsOf(cx, cz int32) *biomeCells {
	key := [2]int32{cx, cz}
	g.surfaceMu.Lock()
	cells, ok := g.cellCache[key]
	if !ok {
		if len(g.cellCache) >= cellCacheMax {
			clear(g.cellCache)
		}
		cells = new(biomeCells)
		g.cellCache[key] = cells
	}
	g.surfaceMu.Unlock()
	cells.once.Do(func() { cells.mask = g.computeBiomes(cx, cz, &cells.grid) })
	return cells
}

// cellReader reads quart cells for one chunk's surface build, keeping the
// last chunk of cells so the shared cache's lock is taken once per chunk
// crossed rather than once per block.
type cellReader struct {
	g      *OverworldGenerator
	cx, cz int32
	cells  *biomeGrid
}

// cell is the biome of a quart cell, clamped to the world.
func (r *cellReader) cell(qx, qy, qz int32) terrain.OverworldBiome {
	qy = min(max(qy, -16), -16+96-1)
	if cx, cz := qx>>2, qz>>2; r.cells == nil || cx != r.cx || cz != r.cz {
		r.cx, r.cz, r.cells = cx, cz, r.g.biomeCellsAt(cx, cz)
	}
	return r.cells[qx&3][qy+16][qz&3]
}

// biomeAt is the biome a block reads. vanilla: BiomeManager.getBiome
func (g *OverworldGenerator) biomeAt(x, y, z int32) terrain.OverworldBiome {
	qx, qy, qz := levelgen.ZoomedQuart(g.zoomSeed, x, y, z)
	return (&cellReader{g: g}).cell(qx, qy, qz)
}

// temperatureAt is a biome's height-adjusted temperature at a block.
func (g *OverworldGenerator) temperatureAt(b terrain.OverworldBiome, x, y, z int32) float32 {
	return heightAdjustedTemperature(g.SeaLevel(), x, y, z, g.biomeClimates[b])
}

// setSurface resolves the material rules' blocks and biome climates once per seed.
func (g *OverworldGenerator) setSurface(seed int64) {
	g.zoomSeed = levelgen.BiomeZoomSeed(seed)
	g.surfaceMu = &sync.Mutex{}
	g.cellCache = map[[2]int32]*biomeCells{}
	for i := range g.biomeClimates {
		g.biomeClimates[i] = registry.Biome(g.biomeIDs[i]).Climate()
	}
	for m := terrain.Air; m <= terrain.Calcite; m++ {
		state, ok := registry.FindState("minecraft:"+m.Name(), nil)
		if !ok {
			panic("the block registry has no " + m.Name())
		}
		g.materialStates[m] = state
	}
}

type overworldMaterialBiomes struct {
	g       *OverworldGenerator
	reader  cellReader
	sampler *terrain.OverworldBiomeSampler
	// column serves the surface's lookups down one block column, which share
	// their corner jitters.
	column     levelgen.ZoomColumn
	colX, colZ int32
	hasColumn  bool
	// layerY is the quart layer that layerBiome and layerUniform describe.
	layerY       int32
	layerBiome   terrain.OverworldBiome
	layerUniform bool
	hasLayer     bool
	// grids are the biome grids of the chunks around the column's quart, [dx][dz]
	// from chunk (gridX, gridZ), so the neighbour reads take no lock.
	grids        [2][2]*biomeGrid
	gridX, gridZ int32
}

// cellNear is the biome of a quart cell within one chunk of the column's.
func (b *overworldMaterialBiomes) cellNear(qx, qy, qz int32) terrain.OverworldBiome {
	dx, dz := qx>>2-b.gridX, qz>>2-b.gridZ
	if uint32(dx) > 1 || uint32(dz) > 1 {
		return b.reader.cell(qx, qy, qz)
	}
	g := b.grids[dx][dz]
	if g == nil {
		g = b.g.biomeCellsAt(qx>>2, qz>>2)
		b.grids[dx][dz] = g
	}
	return g[qx&3][min(max(qy, -16), -16+96-1)+16][qz&3]
}

func (*overworldMaterialBiomes) Index(path string) int {
	for b := range terrain.OverworldBiomeCount {
		if terrain.OverworldBiome(b).Name() == path {
			return b
		}
	}
	return -1
}
func (b *overworldMaterialBiomes) At(x, y, z int32) int {
	if b.sampler != nil {
		qx, qy, qz := levelgen.ZoomedQuart(b.g.zoomSeed, x, y, z)
		return int(b.sampler.At(qx, qy, qz))
	}
	if !b.hasColumn || b.colX != x || b.colZ != z {
		b.column, b.colX, b.colZ, b.hasColumn, b.hasLayer = *levelgen.NewZoomColumn(b.g.zoomSeed, x, z), x, z, true, false
	}
	// The zoom picks among the eight cells around a block; when they hold one
	// biome (the usual case) the jittered distances are skipped.
	px, pz, py := (x-2)>>2, (z-2)>>2, (y-2)>>2
	if gx, gz := px>>2, pz>>2; b.gridX != gx || b.gridZ != gz {
		b.gridX, b.gridZ, b.grids = gx, gz, [2][2]*biomeGrid{}
		b.hasLayer = false
	}
	if !b.hasLayer || b.layerY != py {
		b.layerY, b.hasLayer = py, true
		first := b.cellNear(px, py, pz)
		b.layerBiome, b.layerUniform = first, true
		for i := 1; i < 8 && b.layerUniform; i++ {
			b.layerUniform = b.cellNear(px+int32(i>>2&1), py+int32(i>>1&1), pz+int32(i&1)) == first
		}
	}
	if b.layerUniform {
		return int(b.layerBiome)
	}
	qx, qy, qz := b.column.At(y)
	return int(b.cellNear(qx, qy, qz))
}
func (b *overworldMaterialBiomes) Temperature(biome int, x, y, z, seaLevel int32) float32 {
	return heightAdjustedTemperature(seaLevel, x, y, z, b.g.biomeClimates[biome])
}
func (g *OverworldGenerator) terrainBiomes() terrain.TerrainBiomes {
	surface := &overworldMaterialBiomes{g: g, reader: cellReader{g: g}}
	carving := &overworldMaterialBiomes{g: g, sampler: g.biomeSource.NewSampler()}
	return terrain.TerrainBiomes{Surface: surface, Carving: carving, Carvers: func(int32, int32) []terrain.Carver { return terrain.OverworldCarvers }}
}
