package entity

import (
	"math"
	"math/rand"
	"testing"

	"github.com/golem-mc/golem/registry"
)

type liveSpawnConductorWorld struct {
	spawnTestWorld
	blocked bool
	reads   [][3]int32
}

func (w *liveSpawnConductorWorld) ConductorAt(x, y, z int32) bool {
	w.reads = append(w.reads, [3]int32{x, y, z})
	return w.blocked
}

func TestNaturalSpawnInitialLiveConductor(t *testing.T) {
	for _, blocked := range []bool{true, false} {
		w := &liveSpawnConductorWorld{spawnTestWorld: newSpawnTestWorld(), blocked: blocked}
		w.surface, w.allStone, w.stoneState = 64, true, registry.BlockShulkerBox.DefaultState()
		rng, want := rand.New(rand.NewSource(28)), rand.New(rand.NewSource(28))
		var bodies []SpawnBody
		spawnCategoryForChunk(w, spawnPoint{}, rng, ChunkPos{},
			[]SpawnPlayer{{EntityID: 1, X: 0.5, Y: -15, Z: 0.5}}, &bodies, &SpawnPotential{}, &SpawnCounts{}, CategoryMonster,
			func(SpawnRequest) { t.Fatal("spawned inside the player's exclusion radius") })
		if len(w.reads) != 1 || w.reads[0] != [3]int32{8, -15, 13} {
			t.Fatalf("live conductor reads = %v", w.reads)
		}
		want.Intn(16)
		want.Intn(16)
		want.Intn(130)
		if !blocked {
			// vanilla: NaturalSpawner.spawnCategoryForPosition, before the
			// distance check: three packs, four nextInt(6) calls per step.
			for range 3 {
				steps := int(math.Ceil(float64(want.Float32() * 4)))
				for range steps * 4 {
					want.Intn(6)
				}
			}
		}
		for range 4 {
			if got, expected := rng.Int63(), want.Int63(); got != expected {
				t.Fatalf("blocked=%t: next RNG draw = %d, want %d", blocked, got, expected)
			}
		}
	}
}
