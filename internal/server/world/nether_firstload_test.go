package world

import (
	"strings"
	"testing"
)

// TestJigsawStructuresLoadOnDemand checks that a Nether structure parses
// only the templates its own pools reach.
func TestJigsawStructuresLoadOnDemand(t *testing.T) {
	t.Cleanup(resetJigsawCaches)
	resetJigsawCaches()
	if jigsawStructureNamed("minecraft:bastion_remnant") == nil {
		t.Fatal("no bastion remnant")
	}
	jigsawTemplateCache.mu.Lock()
	defer jigsawTemplateCache.mu.Unlock()
	if len(jigsawTemplateCache.m) == 0 {
		t.Fatal("the bastion parsed no templates")
	}
	for id := range jigsawTemplateCache.m {
		if strings.HasPrefix(id, "minecraft:village/") {
			t.Fatalf("loading the bastion parsed %s", id)
		}
	}
}

// BenchmarkJigsawStructureLoad loads one structure's pools and templates from
// empty caches.
func BenchmarkJigsawStructureLoad(b *testing.B) {
	b.Cleanup(resetJigsawCaches)
	for _, id := range []string{"minecraft:bastion_remnant", "minecraft:village_plains", "minecraft:trial_chambers"} {
		b.Run(strings.TrimPrefix(id, "minecraft:"), func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				resetJigsawCaches()
				if jigsawStructureNamed(id) == nil {
					b.Fatalf("no jigsaw structure %s", id)
				}
			}
		})
	}
}

// BenchmarkNetherGeneratorFirstLoad measures construction before any bastion
// lookup, including the jigsaw load if construction triggers it.
func BenchmarkNetherGeneratorFirstLoad(b *testing.B) {
	// Benchmarks run serially after tests. Reset the caches per iteration so
	// calibration cannot amortize the first load away. Other process caches
	// stay warm.
	b.Cleanup(resetJigsawCaches)
	b.ReportAllocs()
	for b.Loop() {
		resetJigsawCaches()
		NewNetherGenerator(5)
	}
}
