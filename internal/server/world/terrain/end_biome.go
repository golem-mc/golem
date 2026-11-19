package terrain

import "github.com/golem-mc/golem/internal/server/world/levelgen"

// EndBiomeAt picks the biome at a quart position from the erosion read at the
// middle of its chunk; within 64 chunks of the origin it is always the_end.
// vanilla: TheEndBiomeSource.getNoiseBiome
func EndBiomeAt(erosion Sampler, quartX, quartY, quartZ int32) levelgen.EndBiome {
	chunkX, chunkZ := quartX>>2, quartZ>>2
	if int64(chunkX)*int64(chunkX)+int64(chunkZ)*int64(chunkZ) <= 4096 {
		return levelgen.TheEnd
	}
	h := float64(erosion.Sample(NewUncachedContext(), (chunkX*2+1)*8, quartY<<2, (chunkZ*2+1)*8))
	switch {
	case h > 0.25:
		return levelgen.EndHighlands
	case h >= -0.0625:
		return levelgen.EndMidlands
	case h < -0.21875:
		return levelgen.SmallEndIslands
	}
	return levelgen.EndBarrens
}
