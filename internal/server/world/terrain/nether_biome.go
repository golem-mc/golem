package terrain

import "github.com/golem-mc/golem/internal/server/world/levelgen"

// NetherBiomeAt selects the nearest climate entry at a quart position.
// An optional previous result seeds RTree.search and wins exact fitness ties.
// vanilla: MultiNoiseBiomeSource.getNoiseBiome, Climate.RTree.search
func NetherBiomeAt(r Router, quartX, quartY, quartZ int32, previous ...levelgen.NetherBiome) levelgen.NetherBiome {
	c := NewUncachedContext()
	t := int64(float32(r.Temperature.Sample(c, quartX<<2, quartY<<2, quartZ<<2)) * 10000)
	h := int64(float32(r.Vegetation.Sample(c, quartX<<2, quartY<<2, quartZ<<2)) * 10000)
	points := [...]struct {
		t, h, offset int64
		biome        levelgen.NetherBiome
	}{
		{0, 0, 0, levelgen.NetherWastes}, {4000, 0, 0, levelgen.CrimsonForest},
		{0, -5000, 0, levelgen.SoulSandValley}, {-5000, 0, 1750, levelgen.BasaltDeltas},
		{0, 5000, 3750, levelgen.WarpedForest},
	}
	best, distance := levelgen.NetherWastes, int64(-1)
	for _, p := range points {
		dt, dh := t-p.t, h-p.h
		d := dt*dt + dh*dh + p.offset*p.offset
		if distance < 0 || d < distance || d == distance && len(previous) != 0 && p.biome == previous[0] {
			best, distance = p.biome, d
		}
	}
	return best
}
