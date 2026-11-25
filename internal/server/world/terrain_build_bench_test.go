package world

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/world/terrain"
)

type surfaceLookupCounter struct {
	terrain.Biomes
	calls int
}

func (b *surfaceLookupCounter) At(x, y, z int32) int {
	b.calls++
	return b.Biomes.At(x, y, z)
}

// BenchmarkBuildTerrainChunk measures a terrain build with warm biome grids,
// without structures or decoration.
func BenchmarkBuildTerrainChunk(b *testing.B) {
	g := NewOverworldGenerator(12345)
	g.DisableStructures()
	chunk := terrain.NewProtoChunk(worldMinY, worldHeight, terrain.Stone)
	g.state.BuildTerrain(chunk, 0, 0, nil, g.terrainBiomes(0, 0))
	var lookups surfaceLookupCounter
	b.ReportAllocs()
	for b.Loop() {
		chunk.Reset()
		biomes := g.terrainBiomes(0, 0)
		lookups.Biomes = biomes.Surface
		biomes.Surface = &lookups
		g.state.BuildTerrain(chunk, 0, 0, nil, biomes)
	}
	b.ReportMetric(float64(lookups.calls)/float64(b.N), "lookups/chunk")
}
