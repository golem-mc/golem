package world

import (
	"sync"

	"github.com/golem-mc/golem/internal/server/world/terrain"
)

// The overworld's terrain shape: vanilla's final density, filled into the
// chunk as stone, with water and lava where it leaves a block open.

// columnHeights is the Y of each column's topmost solid block, indexed by x
// then z within the chunk.
type columnHeights = [sectionSize][sectionSize]int32

// heightsCacheMax caps the cached chunks; past it the map is dropped.
const heightsCacheMax = 4096

type overworldMaterialChunk struct {
	*terrain.ProtoChunk
	chunk *Chunk
}

func (c overworldMaterialChunk) MarkPosForPostProcessing(x, y, z int32) {
	c.chunk.scheduleGenerated(x&15, y, z&15)
}

// protoChunks recycles the full-height intermediate chunk of fillTerrain.
var protoChunks = sync.Pool{New: func() any {
	return terrain.NewProtoChunk(worldMinY, worldHeight, terrain.Stone)
}}

// fillTerrain builds the merged terrain status, including material rules and carvers.
// vanilla: NoiseBasedChunkGenerator.buildTerrain
func (g *OverworldGenerator) fillTerrain(c *Chunk) {
	if g.flat != nil {
		// vanilla: FlatLevelSource.buildTerrain
		g.flat.fillLayers(c, true)
		return
	}
	proto := protoChunks.Get().(*terrain.ProtoChunk)
	defer protoChunks.Put(proto)
	proto.Reset()
	chunk := overworldMaterialChunk{proto, c}
	g.state.BuildTerrain(chunk, c.x, c.z, g.beardifierAt(c.x, c.z), g.terrainBiomes(c.x, c.z))
	c.fillFromProto(proto, &g.materialStates, true)
}

// chunkHeights caches the top solid block of each base column, before material rules.
// vanilla: NoiseBasedChunkGenerator.getBaseHeight
func (g *OverworldGenerator) chunkHeights(cx, cz int32) *columnHeights {
	key := [2]int32{cx, cz}
	g.heightsMu.Lock()
	h := g.heights[key]
	g.heightsMu.Unlock()
	if h != nil {
		return h
	}
	var heights columnHeights
	for x := range heights {
		for z := range heights[x] {
			heights[x][z] = worldMinY
		}
	}
	nc := terrain.NewNoiseChunk(g.state, nil, g.state.ChunkVolume(cx, cz))
	defer nc.Recycle()
	density := nc.SampleDensity()
	defer nc.ReleaseDensity(density)
	nc.DoFill(density, func(x, y, z int32, s terrain.Substance) {
		if s == terrain.SubstanceDefault {
			heights[x&15][z&15] = max(heights[x&15][z&15], y)
		}
	})
	for x := range heights {
		for z := range heights[x] {
			// Base heights use the aquifer's barrier blocks as solid too.
			heights[x][z] = max(heights[x][z], worldMinY)
		}
	}
	return g.storeHeights(cx, cz, heights)
}

func (g *OverworldGenerator) storeHeights(cx, cz int32, heights columnHeights) *columnHeights {
	g.heightsMu.Lock()
	defer g.heightsMu.Unlock()
	if len(g.heights) >= heightsCacheMax {
		clear(g.heights)
	}
	g.heights[[2]int32{cx, cz}] = &heights
	return &heights
}

// height is the top solid block of the base column, before material rules.
func (g *OverworldGenerator) height(x, z int32) int32 {
	if g.liveHeight != nil {
		return g.liveHeight(x, z)
	}
	if g.flat != nil {
		return g.flat.surfaceY()
	}
	return g.chunkHeights(ChunkOf(float64(x)), ChunkOf(float64(z)))[x&15][z&15]
}

// columnHeight is the height searches read for one column: liveHeight's when
// set, else the cached chunk's when there is one, else one sampled column,
// which equals it.
func (g *OverworldGenerator) columnHeight(x, z int32) int32 {
	if g.liveHeight != nil {
		return g.liveHeight(x, z)
	}
	if g.flat != nil {
		return g.flat.surfaceY()
	}
	g.heightsMu.Lock()
	h := g.heights[[2]int32{ChunkOf(float64(x)), ChunkOf(float64(z))}]
	g.heightsMu.Unlock()
	if h != nil {
		return h[x&15][z&15]
	}
	return max(g.state.TopDefaultY(x, z), worldMinY)
}
