package world

import (
	"bytes"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
)

func testShapedTerrainCache(t *testing.T) {
	var cache shapedTerrainCache
	var calls atomic.Int32
	started, finish := make(chan struct{}), make(chan struct{})
	shape := func(c *Chunk) {
		if calls.Add(1) == 1 {
			close(started)
		}
		<-finish
		c.SetBlock(0, 0, 0, stoneState)
	}
	var wg sync.WaitGroup
	var got [8]*shapedTerrain
	for i := range got {
		wg.Go(func() { got[i] = cache.of(ChunkPos{}, nil, make(chan struct{}, 1), shape) })
	}
	<-started
	synctest.Wait()
	if cache.cached(ChunkPos{}) != nil {
		t.Fatal("shaping entry returned by cached")
	}
	close(finish)
	wg.Wait()
	if calls.Load() != 1 {
		t.Fatalf("shape calls = %d, want 1", calls.Load())
	}
	for _, terrain := range got {
		if terrain != got[0] {
			t.Fatal("single flight returned different terrain")
		}
	}
	for x := int32(-2); x <= 2; x++ {
		for z := int32(-2); z <= 2; z++ {
			cache.markFinal(ChunkPos{X: x, Z: z})
		}
	}
	if cache.cached(ChunkPos{}) != nil || cache.lru.Len() != 0 {
		t.Fatal("completed entry retained")
	}
	cache.cap = 2
	fill := func(c *Chunk) { c.SetBlock(0, 0, 0, stoneState) }
	a, b, c := ChunkPos{X: 10}, ChunkPos{X: 11}, ChunkPos{X: 12}
	cache.of(a, nil, nil, fill)
	cache.of(b, nil, nil, fill)
	cache.cached(a)
	cache.of(c, nil, nil, fill)
	if cache.cached(a) == nil || cache.cached(b) != nil || cache.cached(c) == nil {
		t.Fatal("eviction did not respect LRU order")
	}
}

func TestGenerationWithTerrainEviction(t *testing.T) {
	for _, dim := range []string{"overworld", "nether"} {
		t.Run(dim, func(t *testing.T) {
			var generate [2]func(*Chunk)
			if dim == "overworld" {
				a, b := NewOverworldGenerator(777), NewOverworldGenerator(777)
				a.cache.cap, b.cache.cap = 4096, 2
				generate = [2]func(*Chunk){a.Generate, b.Generate}
			} else {
				a, b := NewNetherGenerator(777), NewNetherGenerator(777)
				a.cache.cap, b.cache.cap = 4096, 2
				generate = [2]func(*Chunk){a.Generate, b.Generate}
			}
			for _, pos := range []ChunkPos{{X: 3, Z: -3}, {X: 3, Z: -2}, {X: 4, Z: -2}} {
				a, b := newChunk(pos.X, pos.Z), newChunk(pos.X, pos.Z)
				generate[0](a)
				generate[1](b)
				if !bytes.Equal(encodeWhole(a), encodeWhole(b)) {
					t.Fatalf("chunk %v differs after terrain eviction", pos)
				}
				if !slices.Equal(a.pendingGenerated, b.pendingGenerated) {
					t.Fatalf("chunk %v fluid visits differ after terrain eviction", pos)
				}
			}
		})
	}
}
