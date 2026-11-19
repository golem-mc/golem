package entity

import (
	"slices"
	"testing"

	"github.com/golem-mc/golem/registry"
)

func TestNaturalSpawnBiomeClasses(t *testing.T) {
	merged := 0
	for c := range SpawnCategory(SpawnCategoryCount) {
		reduce := func(b registry.Biome) bool {
			return c == CategoryWaterAmbient && b.InTag(registry.BiomeTagIDReduceWaterAmbientSpawns)
		}
		for b := range registry.Biome(registry.BiomeCount) {
			r := naturalBiomeRepresentatives[c][b]
			if c == CategoryWaterAmbient && r != b {
				t.Fatalf("water ambient biome %d merged into %d; suppression reads reloadable tags", b, r)
			}
			if r > b || naturalBiomeRepresentatives[c][r] != r {
				t.Fatalf("category %d biome %d: representative %d is not a lower class head", c, b, r)
			}
			if !slices.Equal(naturalBiomeLists[c][r].entries, naturalBiomeLists[c][b].entries) ||
				naturalBiomeLists[c][r].total != naturalBiomeLists[c][b].total ||
				!slices.Equal(biomeSpawnCosts[r], biomeSpawnCosts[b]) || reduce(r) != reduce(b) {
				t.Fatalf("category %d biome %d differs from representative %d", c, b, r)
			}
			// The lowest biome of the class must be the representative.
			for lower := range b {
				if c != CategoryWaterAmbient && naturalBiomesEquivalent(c, lower, b) && r > lower {
					t.Fatalf("category %d biome %d: lower equivalent %d, representative %d", c, b, lower, r)
				}
			}
			if r != b {
				merged++
			}
		}
	}
	if merged == 0 {
		t.Fatal("no biome shares a class")
	}
}
