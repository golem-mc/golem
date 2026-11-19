package world

import (
	"bytes"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
)

func TestShapedTerrainCache(t *testing.T) {
	synctest.Test(t, testShapedTerrainCache)
}

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

// Uniform sections cost no block words, but still own terrain, fluid visits
// and cache metadata. The entry cap must bound those chunks too.
func TestShapedTerrainCacheEntryLimit(t *testing.T) {
	var cache shapedTerrainCache
	fill := func(c *Chunk) { c.pendingGenerated = []posKey{{1, 2, 3}, {4, 5, 6}} }
	held := cache.of(ChunkPos{}, nil, nil, fill)
	for x := int32(1); x <= shapedTerrainCap; x++ {
		cache.of(ChunkPos{X: x}, nil, nil, fill)
	}
	if len(cache.byPos) != shapedTerrainCap || cache.lru.Len() != shapedTerrainCap {
		t.Fatalf("retained %d entries and %d LRU nodes, limit %d", len(cache.byPos), cache.lru.Len(), shapedTerrainCap)
	}
	if cache.cached(ChunkPos{}) != nil {
		t.Fatal("least recent terrain retained")
	}
	c := newChunk(0, 0)
	held.restore(c)
	if !slices.Equal(c.pendingGenerated, []posKey{{1, 2, 3}, {4, 5, 6}}) {
		t.Fatal("eviction invalidated held terrain")
	}
	c.pendingGenerated[0] = posKey{}
	if held.fluidUpdates[0] != (posKey{1, 2, 3}) {
		t.Fatal("restore aliases cached fluid visits")
	}
}

func TestShapedTerrainCachePendingEviction(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		cache := shapedTerrainCache{cap: 1}
		started, finish := make(chan struct{}), make(chan struct{})
		var calls atomic.Int32
		shape := func(c *Chunk) {
			calls.Add(1)
			close(started)
			<-finish
			c.SetBlock(0, 0, 0, stoneState)
		}
		var a, b *shapedTerrain
		var wg sync.WaitGroup
		wg.Go(func() { a = cache.of(ChunkPos{}, nil, nil, shape) })
		<-started
		cache.of(ChunkPos{X: 1}, nil, nil, func(*Chunk) {})
		wg.Go(func() { b = cache.of(ChunkPos{}, nil, nil, shape) })
		synctest.Wait()
		close(finish)
		wg.Wait()
		if a != b || calls.Load() != 1 {
			t.Fatal("eviction broke pending single flight")
		}
		if len(cache.byPos) > 1 {
			t.Fatal("completed shapes exceed the cap")
		}
		c := newChunk(0, 0)
		a.restore(c)
		c.SetBlock(0, 0, 0, airState)
		if a.blockAt(0, 0, 0) != stoneState {
			t.Fatal("restoration changed held terrain")
		}
	})
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
