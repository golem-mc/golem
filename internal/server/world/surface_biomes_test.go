package world

import (
	"slices"
	"sync"
	"testing"

	"github.com/golem-mc/golem/internal/server/world/terrain"
)

type surfaceOutcome struct {
	*terrain.ProtoChunk
	post [][3]int32
	mask [][4]int32
}

func (c *surfaceOutcome) MarkPosForPostProcessing(x, y, z int32) {
	c.post = append(c.post, [3]int32{x, y, z})
}

func TestSurfaceBiomePruningMatchesUnpruned(t *testing.T) {
	g := NewOverworldGenerator(12345)
	g.DisableStructures()
	positions := [][2]int32{{0, 0}, {8, 8}}
	build := func(p [2]int32, unpruned bool) *surfaceOutcome {
		c := &surfaceOutcome{ProtoChunk: terrain.NewProtoChunk(worldMinY, worldHeight, terrain.Stone)}
		// Each build owns its surface reader and stateful carving sampler.
		biomes := g.terrainBiomes()
		// vanilla: ChunkStatusTasks.collectPossibleBiomes includes all vertical
		// palettes of the nine chunks, even where the zoom never selects them.
		var possible uint64
		for dz := int32(-1); dz <= 1; dz++ {
			for dx := int32(-1); dx <= 1; dx++ {
				possible |= g.biomeCellsOf(p[0]+dx, p[1]+dz).mask
			}
		}
		biomes.PossibleBiomes = make([]int, 0)
		for b := range terrain.OverworldBiomeCount {
			if possible&(1<<b) != 0 {
				biomes.PossibleBiomes = append(biomes.PossibleBiomes, b)
			}
		}
		if unpruned {
			biomes.PossibleBiomes = nil
		}
		mask := g.state.BuildTerrain(c, p[0], p[1], nil, biomes)
		mask.Visit(func(x, z, bottom, top int32) { c.mask = append(c.mask, [4]int32{x, z, bottom, top}) })
		return c
	}
	compare := func(a, b *surfaceOutcome) {
		t.Helper()
		for x := range int32(16) {
			for z := range int32(16) {
				if !slices.Equal(a.Column(x, z), b.Column(x, z)) {
					t.Fatalf("materials differ in column %d,%d", x, z)
				}
				if a.Height(x, z) != b.Height(x, z) {
					t.Fatalf("height differs at %d,%d", x, z)
				}
			}
		}
		if !slices.Equal(a.mask, b.mask) {
			t.Fatal("ordered carving mask differs")
		}
		if !slices.Equal(a.post, b.post) {
			t.Fatal("ordered deferred fluid callbacks differ")
		}
	}
	serial := make([]*surfaceOutcome, len(positions))
	for i, p := range positions {
		serial[i] = build(p, false)
		compare(serial[i], build(p, true))
	}
	// Two pairs per position also overlap readers of the same cached grids.
	results := make([][2]*surfaceOutcome, len(positions)*2)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := range results {
		wg.Go(func() {
			<-start
			p := positions[i%len(positions)]
			results[i] = [2]*surfaceOutcome{build(p, false), build(p, true)}
		})
	}
	close(start)
	wg.Wait()
	for i, pair := range results {
		compare(pair[0], pair[1])
		compare(pair[0], serial[i%len(positions)])
	}
}
