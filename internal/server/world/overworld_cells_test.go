package world

import "testing"

// TestBiomeCellsEvictLeastRecentlyUsed fills more than cellCacheMax chunks in
// a ring: the oldest entry goes, the most recently touched one stays, and a
// refilled chunk reads the same grid.
func TestBiomeCellsEvictLeastRecentlyUsed(t *testing.T) {
	g := NewOverworldGenerator(8675309)
	// Filler entries are inserted already filled, to skip computeBiomes.
	first := g.biomeCellsOf(0, 0)
	want := first.grid
	for i := range int32(cellCacheMax - 1) {
		key := [2]int32{1000 + i, 1000}
		g.surfaceMu.Lock()
		c := &biomeCells{key: key, done: true}
		c.once.Do(func() {})
		c.elem = g.cellLRU.PushFront(c)
		g.cellCache[key] = c
		g.surfaceMu.Unlock()
	}
	g.biomeCellsOf(0, 0) // touch the oldest: it is now the most recent
	g.biomeCellsOf(5, 5) // one past the cap evicts the least recent
	g.surfaceMu.Lock()
	_, keptFirst := g.cellCache[[2]int32{0, 0}]
	_, keptOldest := g.cellCache[[2]int32{1000, 1000}]
	n := len(g.cellCache)
	g.surfaceMu.Unlock()
	if !keptFirst {
		t.Error("the most recently used entry was evicted")
	}
	if keptOldest {
		t.Error("the least recently used entry survived")
	}
	if n != cellCacheMax {
		t.Errorf("cache holds %d entries, want %d", n, cellCacheMax)
	}
	// Evict (0,0) for real, then refill it.
	for i := range int32(cellCacheMax) {
		key := [2]int32{3000 + i, 3000}
		g.surfaceMu.Lock()
		c := &biomeCells{key: key, done: true}
		c.once.Do(func() {})
		c.elem = g.cellLRU.PushFront(c)
		g.cellCache[key] = c
		g.evictCells()
		g.surfaceMu.Unlock()
	}
	g.surfaceMu.Lock()
	_, still := g.cellCache[[2]int32{0, 0}]
	g.surfaceMu.Unlock()
	if still {
		t.Fatal("entry (0,0) should have been evicted")
	}
	if got := *g.biomeCellsAt(0, 0); got != want {
		t.Error("refilled chunk differs from the first fill")
	}
}
