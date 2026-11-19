package world

import (
	"slices"
	"strings"
	"sync"
	"testing"
)

var testTerrains sync.Map // testTerrainKey -> func() Generator

type testTerrainKey struct {
	seed        int64
	springsOnly bool
}

// testTerrain gives block-only readers independent arrays from one generated
// terrain, without repeating spawn selection, structures or decoration.
func testTerrain(seed int64, springsOnly bool) Generator {
	build, _ := testTerrains.LoadOrStore(testTerrainKey{seed, springsOnly}, sync.OnceValue(func() Generator {
		return newTestTerrain(seed, springsOnly)
	}))
	return build.(func() Generator)()
}

func newTestTerrain(seed int64, springsOnly bool) Generator {
	g := NewOverworldGenerator(seed)
	// Lighting compares block arrays; structures are not part of its oracle.
	g.DisableStructures()
	if springsOnly {
		g.placed = slices.DeleteFunc(g.placed, func(p overworldPlaced) bool { return !strings.Contains(p.feature.Name, "spring") })
	}
	return &testTerrainCache{gen: g, chunks: make(map[ChunkPos]*shapedTerrain)}
}

type testTerrainCache struct {
	mu     sync.Mutex
	gen    Generator
	chunks map[ChunkPos]*shapedTerrain
}

func (g *testTerrainCache) SeaLevel() int32 { return g.gen.SeaLevel() }

func (g *testTerrainCache) Generate(c *Chunk) {
	g.mu.Lock()
	defer g.mu.Unlock()
	pos := ChunkPos{c.x, c.z}
	terrain := g.chunks[pos]
	if terrain == nil {
		original := newChunk(c.x, c.z)
		g.gen.Generate(original)
		terrain = &shapedTerrain{}
		terrain.keep(original, false)
		g.chunks[pos] = terrain
	}
	terrain.restore(c)
}

func TestCachedTerrainReadersHaveIndependentBlocks(t *testing.T) {
	t.Parallel()
	gen := testTerrain(11, false)
	a, b, fresh := newChunk(0, 0), newChunk(0, 0), newChunk(0, 0)
	gen.Generate(a)
	gen.Generate(b)
	before := b.BlockAt(0, 0, 0)
	// Exercise an allocated section as well as an empty section above terrain.
	for _, y := range []int32{0, 200} {
		a.SetBlock(0, y, 0, torchState)
	}
	gen.Generate(fresh)
	if b.BlockAt(0, 0, 0) != before || fresh.BlockAt(0, 0, 0) != before ||
		b.BlockAt(0, 200, 0) != airState || fresh.BlockAt(0, 200, 0) != airState {
		t.Fatal("a cached terrain reader changed another reader or the source")
	}
}
