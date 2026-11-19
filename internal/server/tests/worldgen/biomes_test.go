package worldgen

import (
	"testing"

	"github.com/golem-mc/golem/internal/server"

	"github.com/golem-mc/golem/internal/server/world/terrain"
)

// overworldBiomesAreTheClimateBiomes checks the biome a seeded overworld
// carries at the surface of every column of a square against vanilla's
// multi-noise biome source built from the same seed (pinned to vanilla's own
// classes by terrain's golden test): the cell a column's surface falls in
// reads the nearest entry of the overworld's parameter list. The square of
// seed 12345 around x 208..303, z 80..127 holds several biomes.
func overworldBiomesAreTheClimateBiomes(t *testing.T, srv *server.Server) {
	source := terrain.NewOverworldBiomeSource(terrain.NewRandomState(terrain.Overworld, 12345))

	// Each chunk's cells in the order vanilla fills them, which decides the
	// cells the climate is equally near two entries of.
	type chunkKey struct{ cx, cz int32 }
	chunks := map[chunkKey]*[4][96][4]terrain.OverworldBiome{}
	cell := func(x, y, z int32) terrain.OverworldBiome {
		k := chunkKey{x >> 4, z >> 4}
		c := chunks[k]
		if c == nil {
			c = new([4][96][4]terrain.OverworldBiome)
			source.FillChunk(k.cx, k.cz, func(x, y, z int32, b terrain.OverworldBiome) { c[x][y][z] = b })
			chunks[k] = c
		}
		return c[(x>>2)&3][(y>>2)+16][(z>>2)&3]
	}

	var columns, mismatches int
	seen := map[string]int{}
	srv.ScanOverworldSurfaces(208, 80, 303, 127, 200, func(x, y, z int32, _ uint16) bool {
		columns++
		got := srv.OverworldNoiseBiomeAt(x, y, z)
		seen[got]++
		if want := "minecraft:" + cell(x, y, z).Name(); got != want {
			mismatches++
			if mismatches <= 8 {
				t.Errorf("the surface at (%d, %d, %d) reads %s, the biome source says %s", x, y, z, got, want)
			}
		}
		return false
	})
	if columns == 0 {
		t.Fatal("the square carries no terrain at all")
	}
	if mismatches > 0 {
		t.Errorf("%d of %d surface columns read a biome other than the biome source's", mismatches, columns)
	}
	if len(seen) < 3 {
		t.Errorf("the square reads %d biomes (%v), want several", len(seen), seen)
	}
}
