package world

import (
	"slices"
	"strings"
	"sync"
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
