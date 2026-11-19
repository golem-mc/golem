package world

import (
	"testing"

	"github.com/golem-mc/golem/registry"
)

func BenchmarkFeatureChangesDigest(b *testing.B) {
	terrain := newGoldenTerrain()
	r := newDecorRegion(terrain, newChunk(-1, 2), worldMinY, worldHeight)
	// Sparse writes and no-ops across chunks and sections resemble placed
	// features; negative coordinates exercise signed coordinate formatting.
	for x := int32(-32); x < 16; x += 3 {
		for z := int32(16); z < 64; z += 3 {
			for y := int32(48); y < 80; y += 3 {
				r.set(x, y, z, registry.BlockStone.DefaultState())
			}
		}
	}
	want, n := terrain.changes(r)
	if n == 0 {
		b.Fatal("no changed blocks")
	}
	b.ReportAllocs()
	for b.Loop() {
		if got, count := terrain.changes(r); got != want || count != n {
			b.Fatal("digest changed")
		}
	}
}
