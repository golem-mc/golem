package terrain

import (
	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/server/world/levelgen"
)

type countedMaterialNoise struct {
	Noise
	calls int
}

func (n *countedMaterialNoise) Get3D(x, y, z float64) float32 {
	n.calls++
	return float32(x + y + z)
}

func TestBadlandsBuildSurfaceSamplesClayBandsOncePerColumn(t *testing.T) {
	rs := NewRandomState(Overworld, 12345)
	sample := &countedMaterialNoise{}
	var builder NoiseStackBuilder
	rs.Materials.clayBandsOffset = builder.Add(sample, 1, 1).Build()
	chunk, _ := buildGoldenChunk("overworld", 0, 0, 125, 3)
	biomes := &goldenBiomeSource{names: []string{"badlands"}, zoom: levelgen.BiomeZoomSeed(rs.Seed), melt: strings.Repeat("0", 256)}
	nc := NewNoiseChunk(rs, nil, rs.ChunkVolume(0, 0))
	defer nc.Recycle()

	rs.Materials.BuildSurface(chunk, 0, 0, nc, biomes, []int{0})

	if want := 16 * 16; sample.calls != want {
		t.Errorf("clay-band noise sampled %d times for %d columns", sample.calls, want)
	}
}

type countedMaterialBiomes struct {
	goldenBiomeSource
	biome, calls int
}

func (b *countedMaterialBiomes) At(_, _, _ int32) int {
	b.calls++
	return b.biome
}

type extensionMaterialNoise struct{ Noise }

func (*extensionMaterialNoise) Get3D(_, _, _ float64) float32 { return 1 }
