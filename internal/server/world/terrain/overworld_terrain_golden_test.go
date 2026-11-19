package terrain

import "github.com/golem-mc/golem/internal/server/world/levelgen"

// naturalGoldenBiomes mirrors the chunk-backed surface lookup and the
// uncached carver lookup of NoiseBasedChunkGenerator.buildTerrain.
type naturalGoldenBiomes struct {
	source  *OverworldBiomeSource
	zoom    int64
	chunks  map[[2]int32]*[4][96][4]OverworldBiome
	melt    string
	sampler *OverworldBiomeSampler
}

func newNaturalGoldenBiomes(rs *RandomState, melt string, uncached bool) *naturalGoldenBiomes {
	b := &naturalGoldenBiomes{
		source: NewOverworldBiomeSource(rs), zoom: levelgen.BiomeZoomSeed(rs.Seed),
		chunks: map[[2]int32]*[4][96][4]OverworldBiome{}, melt: melt,
	}
	if uncached {
		b.sampler = b.source.NewSampler()
	}
	return b
}

func (b *naturalGoldenBiomes) carvers(cx, cz int32) []Carver {
	b.sampler.At(cx*4, 0, cz*4)
	return OverworldCarvers
}

func (*naturalGoldenBiomes) Index(path string) int {
	for i, name := range overworldBiomeNames {
		if path == name {
			return i
		}
	}
	return -1
}

func (b *naturalGoldenBiomes) At(x, y, z int32) int {
	qx, qy, qz := levelgen.ZoomedQuart(b.zoom, x, y, z)
	if b.sampler != nil {
		return int(b.sampler.At(qx, qy, qz))
	}
	key := [2]int32{qx >> 2, qz >> 2}
	grid := b.chunks[key]
	if grid == nil {
		grid = new([4][96][4]OverworldBiome)
		b.source.FillChunk(key[0], key[1], func(x, y, z int32, biome OverworldBiome) {
			grid[x][y][z] = biome
		})
		b.chunks[key] = grid
	}
	return int(grid[qx&3][min(max(qy+16, 0), 95)][qz&3])
}

func (b *naturalGoldenBiomes) Temperature(_ int, x, _, z, _ int32) float32 {
	if b.melt[(z&15)*16+(x&15)] == '1' {
		return 0.2
	}
	return 0
}
