package world

import (
	"slices"
	"testing"
)

func TestColdTerrainRoundTrip(t *testing.T) {
	t.Parallel()
	g := NewOverworldGenerator(4242)
	var cache shapedTerrainCache
	cache.cap = 1
	shape := func(c *Chunk) { g.shapeTerrain(c) }
	a, b := ChunkPos{X: 5, Z: 7}, ChunkPos{X: 6, Z: 7}
	orig := cache.of(a, nil, nil, shape)
	// Reads wg like decoration does, so the arrays are not all zero.
	orig.wg[0][3], orig.wg[1][200] = 77, 12
	want := *orig
	cache.of(b, nil, nil, shape)
	if cache.cold[a] == nil || cache.byPos[a] != nil || cache.coldBytes == 0 {
		t.Fatal("evicted entry not in the cold tier")
	}
	cache.markFinal(ChunkPos{X: 5, Z: 8}) // a is at (0,-1) of it
	got := cache.of(a, nil, nil, func(*Chunk) { t.Fatal("shaped a chunk the cold tier held") })
	if cache.cold[a] != nil {
		t.Fatal("cold entry kept after restore")
	}
	if got == orig {
		t.Fatal("restore returned the evicted terrain")
	}
	for x := range int32(16) {
		for z := range int32(16) {
			for y := int32(worldMinY); y < worldMinY+worldHeight; y++ {
				if got.blockAt(x, y, z) != want.blockAt(x, y, z) {
					t.Fatalf("block %d,%d,%d differs", x, y, z)
				}
			}
		}
	}
	if got.wg != want.wg || !slices.Equal(got.fluidUpdates, want.fluidUpdates) {
		t.Fatal("wg or fluid updates differ")
	}
	if cache.byPos[a].finals != 1<<(3*5+2) {
		t.Fatalf("finals = %b, want the markFinal bit kept", cache.byPos[a].finals)
	}
	// Evict a again, then finish its 5x5: the cold entry goes.
	cache.of(ChunkPos{X: 40}, nil, nil, shape)
	if cache.cold[a] == nil {
		t.Fatal("a not cold again")
	}
	for dx := int32(-2); dx <= 2; dx++ {
		for dz := int32(-2); dz <= 2; dz++ {
			cache.markFinal(ChunkPos{X: a.X + dx, Z: a.Z + dz})
		}
	}
	if cache.cold[a] != nil {
		t.Fatal("completed cold entry retained")
	}
}
