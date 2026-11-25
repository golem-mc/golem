package terrain

import (
	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/server/world/levelgen"
)

// The carvers cut through grass and dirt in this seed 12345 chunk.
const materialBenchChunkX, materialBenchChunkZ = 6, -4

// BenchmarkMaterialTerrainChunk measures fill, surface and carving material
// evaluation with a precomputed mask.
func BenchmarkMaterialTerrainChunk(b *testing.B) {
	rs := NewRandomState(Overworld, 12345)
	biomes := &goldenBiomeSource{names: []string{"plains"}, zoom: levelgen.BiomeZoomSeed(rs.Seed), melt: strings.Repeat("0", 256)}
	mask := rs.CarvingMask(materialBenchChunkX, materialBenchChunkZ, func(int32, int32) []Carver { return OverworldCarvers })
	volume := rs.ChunkVolume(materialBenchChunkX, materialBenchChunkZ)
	materials := [...]Material{SubstanceDefault: Stone, SubstanceAir: Air, SubstanceWater: Water, SubstanceLava: Lava}
	b.ReportAllocs()
	for b.Loop() {
		chunk := NewProtoChunk(rs.MinY, rs.Height, Stone)
		nc := NewNoiseChunk(rs, nil, volume)
		density := nc.SampleDensity()
		nc.DoFill(density, func(x, y, z int32, s Substance) { chunk.Set(x, y, z, materials[s]) })
		nc.ctx.release(density)
		rs.Materials.BuildSurface(chunk, materialBenchChunkX, materialBenchChunkZ, nc, biomes, []int{0})
		rs.applyCarvingMask(chunk, materialBenchChunkX, materialBenchChunkZ, mask, nc, biomes, nil)
		nc.Recycle()
	}
}

// BenchmarkMaterialBuildSurface isolates the material walk, including density
// prefill, on fresh synthetic solid plains columns with a fixed seed.
func BenchmarkMaterialBuildSurface(b *testing.B) {
	rs := NewRandomState(Overworld, 12345)
	biomes := &goldenBiomeSource{names: []string{"plains"}, zoom: levelgen.BiomeZoomSeed(rs.Seed), melt: strings.Repeat("0", 256)}
	template, _ := buildGoldenChunk("overworld", 0, 0, 70, 3)
	chunk := NewProtoChunk(rs.MinY, rs.Height, Stone)
	b.ResetTimer()
	for b.Loop() {
		copy(chunk.blocks, template.blocks)
		chunk.tops = template.tops
		nc := NewNoiseChunk(rs, nil, rs.ChunkVolume(0, 0))
		rs.Materials.BuildSurface(chunk, 0, 0, nc, biomes, []int{0})
	}
}

// BenchmarkBuildTerrainChunk runs the whole terrain step, fill, surface and
// carvers, on fresh overworld chunks with a fixed seed.
func BenchmarkBuildTerrainChunk(b *testing.B) {
	rs := NewRandomState(Overworld, 12345)
	melt := strings.Repeat("0", 256)
	biomes := TerrainBiomes{
		Surface:        &goldenBiomeSource{names: []string{"plains"}, zoom: levelgen.BiomeZoomSeed(rs.Seed), melt: melt},
		Carving:        &goldenBiomeSource{names: []string{"plains"}, zoom: levelgen.BiomeZoomSeed(rs.Seed), melt: melt},
		PossibleBiomes: []int{0},
		Carvers:        func(int32, int32) []Carver { return OverworldCarvers },
	}
	b.ReportAllocs()
	for b.Loop() {
		rs.BuildTerrain(NewProtoChunk(rs.MinY, rs.Height, Stone), materialBenchChunkX, materialBenchChunkZ, nil, biomes)
	}
}
