package world_test

import (
	"math/rand"
	"testing"

	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/world"
)

type emptySpawnGenerator struct{}

func (emptySpawnGenerator) Generate(*world.Chunk) {}
func (emptySpawnGenerator) SeaLevel() int32       { return 63 }

// vanilla: NaturalSpawner.getRandomPosWithin, spawnCategoryForChunk
func TestNaturalSpawnEmptyColumnHeight(t *testing.T) {
	w, err := world.NewWorld(t.TempDir(), emptySpawnGenerator{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = w.Close() })
	pos := world.ChunkPos{X: -1, Z: 1}
	w.Acquire(pos)
	defer w.Release(pos)
	if got, want := w.SurfaceHeight(-16, 16), w.MinY()-1; got != want {
		t.Fatalf("empty surface = %d, want %d", got, want)
	}
	for _, held := range []bool{false, true} {
		for seed := int64(1); seed <= 300; seed++ {
			var sw entity.SpawnWorld = fallbackSpawnWorld{w}
			if held {
				sw = w
			}
			actual, expected := rand.New(rand.NewSource(seed)), rand.New(rand.NewSource(seed))
			counts := entity.SpawnCounts{}
			entity.SpawnNaturally(sw, actual, []entity.ChunkPos{{X: pos.X, Z: pos.Z}},
				[]entity.SpawnPlayer{{EntityID: 1, X: -8, Y: 64, Z: 24}}, nil, &entity.SpawnPotential{},
				[]entity.SpawnPass{entity.NewSpawnPass(entity.CategoryMonster, &counts, -1)}, 289, nil,
				func(entity.SpawnRequest) { t.Fatal("spawned in an empty chunk") })
			// The inclusive range [minY, minY] still consumes nextInt(1),
			// then the floor guard returns without drawing a pack walk.
			expected.Intn(16)
			expected.Intn(16)
			expected.Intn(1)
			for range 4 {
				if got, want := actual.Int63(), expected.Int63(); got != want {
					t.Fatalf("held=%t seed=%d: RNG tail = %d, want %d; empty column walked above minY", held, seed, got, want)
				}
			}
		}
	}
}
