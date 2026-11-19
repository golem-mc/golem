package terrain

import (
	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/server/world/levelgen"
)

// BenchmarkMaterialTerrainChunk includes fill because ore veins are part of
// fill in levelgen and part of BuildSurface here. Carvers and features are excluded.
func BenchmarkMaterialTerrainChunk(b *testing.B) {
	rs := NewRandomState(Overworld, 12345)
	biomes := &goldenBiomeSource{names: []string{"plains"}, zoom: levelgen.BiomeZoomSeed(rs.Seed), melt: strings.Repeat("0", 256)}
	materials := [...]Material{SubstanceDefault: Stone, SubstanceAir: Air, SubstanceWater: Water, SubstanceLava: Lava}
	b.ResetTimer()
	for b.Loop() {
		chunk := NewProtoChunk(rs.MinY, rs.Height, Stone)
		nc := NewNoiseChunk(rs, nil, rs.ChunkVolume(0, 0))
		nc.DoFill(nc.SampleDensity(), func(x, y, z int32, s Substance) { chunk.Set(x, y, z, materials[s]) })
		rs.Materials.BuildSurface(chunk, 0, 0, nc, biomes, []int{0})
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
		rs.BuildTerrain(NewProtoChunk(rs.MinY, rs.Height, Stone), 0, 0, nil, biomes)
	}
}
