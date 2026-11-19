package entity

import (
	"fmt"
	"math/rand"
	"testing"

	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

type spawnHeightGenerator struct{ surface int32 }

func (g spawnHeightGenerator) Generate(c *world.Chunk) {
	c.Fill(-64, g.surface+1, registry.BlockStone.DefaultState())
}
func (spawnHeightGenerator) SeaLevel() int32 { return 63 }

// vanilla: NaturalSpawner.getRandomPosWithin, ChunkAccess.getHeight
func TestNaturalSpawnHeldInitialHeightDice(t *testing.T) {
	for _, surface := range []int32{-65, -61, 63, 64} {
		t.Run(fmt.Sprint(surface), func(t *testing.T) {
			w, err := world.NewWorld(t.TempDir(), spawnHeightGenerator{surface})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = w.Close() })
			pos := world.ChunkPos{X: -1, Z: 1}
			w.Acquire(pos)
			defer w.Release(pos)
			held, ok := w.HoldSpawnChunk(pos)
			if !ok {
				t.Fatal("missing spawn chunk")
			}
			defer held.Close()
			if got := held.OriginSurfaceHeight(0, 0); got != surface {
				t.Fatalf("surface = %d, want %d", got, surface)
			}
			seen := make(map[int32]bool)
			for seed := int64(1); seed <= 2000; seed++ {
				actual, expected := rand.New(rand.NewSource(seed)), rand.New(rand.NewSource(seed))
				// The heightmap returns the highest occupied coordinate; the
				// inclusive upper bound is the first empty block above it.
				x := pos.X*16 + int32(expected.Intn(16))
				z := pos.Z*16 + int32(expected.Intn(16))
				y := w.MinY() + int32(expected.Intn(int(surface+1-w.MinY()+1)))
				seen[y] = true
				if y >= w.MinY()+1 && !registry.IsRedstoneConductorState(w.BlockAt(x, y, z)) {
					// Isolate the initial sampling from the already-tested pack walk.
					spawnCategoryForPosition(w, spawnPoint{}, expected, ChunkPos{X: pos.X, Z: pos.Z}, y, x, z,
						nil, nil, nil, nil, CategoryMonster, nil)
				}
				c := naturalSpawnContext{w: &held, chunk: &held}
				c.spawnCategoryForChunk(spawnPoint{}, actual, ChunkPos{X: pos.X, Z: pos.Z}, nil, nil, nil, nil, CategoryMonster, nil)
				for range 4 {
					if actual.Int63() != expected.Int63() {
						t.Fatalf("seed %d, expected initial y=%d: different RNG tail", seed, y)
					}
				}
			}
			for y := w.MinY(); y <= surface+1; y++ {
				if !seen[y] {
					t.Fatalf("seeds did not exercise y=%d", y)
				}
			}
		})
	}
}
