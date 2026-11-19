package world

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/world/terrain"
)

type terrainBenchBiomes struct{ g *OverworldGenerator }

func (b terrainBenchBiomes) Index(path string) int {
	for i := range terrain.OverworldBiomeCount {
		if terrain.OverworldBiome(i).Name() == path {
			return i
		}
	}
	return -1
}
func (b terrainBenchBiomes) At(x, y, z int32) int { return int(b.g.biomeAt(x, y, z)) }
func (b terrainBenchBiomes) Temperature(biome int, x, y, z, _ int32) float32 {
	return b.g.temperatureAt(terrain.OverworldBiome(biome), x, y, z)
}

// BenchmarkBuildTerrainChunk includes fresh chunk allocation, fill, surface,
// carver generation and mask application, without structures or decoration.
func BenchmarkBuildTerrainChunk(b *testing.B) {
	g := NewOverworldGenerator(12345)
	g.DisableStructures()
	rs := terrain.NewRandomState(terrain.Overworld, 12345)
	biomes := terrainBenchBiomes{g}
	config := terrain.TerrainBiomes{
		Surface: biomes, Carving: biomes,
		Carvers: func(int32, int32) []terrain.Carver { return terrain.OverworldCarvers },
	}
	b.ResetTimer()
	for b.Loop() {
		chunk := terrain.NewProtoChunk(rs.MinY, rs.Height, terrain.Stone)
		rs.BuildTerrain(chunk, 0, 0, nil, config)
	}
}
