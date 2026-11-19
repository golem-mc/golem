package mobs

import (
	"math"
	"math/rand"
	"slices"
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// vanilla: NaturalSpawner.spawnCategoryForPosition. Inside the player's
// exclusion radius, each pack consumes only its size float and four dice per
// step; terrain and mob selection cannot consume any draws.
func TestNaturalSpawnInitialStairsVanillaWalk(t *testing.T) {
	t.Parallel()
	srv, _ := zombieTests(t, servertest.SpawningTestConfig())
	srv.SetOverworldBorder(8, 8, 16)
	// Keep world-spawn protection from masking the player-distance check.
	spawn := srv.WorldSpawn()
	spawn.X += 1000
	srv.SetWorldSpawn(spawn)
	srv.SetGameRule("spawn_phantoms", false)
	// Park the cat countdown and avoid the siege's midnight roll, leaving
	// this pass's stream entirely to NaturalSpawner.
	srv.CatSpawnerPass(1)
	srv.SetDayTime(midnight + 1)
	const seed = 662
	const x, y, z = 12, 64, 2
	stairs := registry.BlockOakStairs.DefaultState()
	if !srv.SetBlock(x, y, z, stairs) {
		t.Fatal("initial spawn chunk is not loaded")
	}

	// vanilla: NaturalSpawner.getRandomPosWithin, ChunkAccess.getHeight.
	// The stairs raise this column's highest block to 64, so the inclusive
	// range [-64, 65] has 130 heights. Replay vanilla's draw schedule with
	// the server's Go RNG, not Java's seed mapping.
	rng := rand.New(rand.NewSource(seed))
	if bx, bz, by := rng.Intn(16), rng.Intn(16), -64+rng.Intn(130); bx != x || by != y || bz != z {
		t.Fatalf("initial position = %d %d %d", bx, by, bz)
	}
	var walk [][2]int
	for range 3 {
		bx, bz := x, z
		for steps := int(math.Ceil(float64(rng.Float32() * 4))); steps > 0; steps-- {
			bx += rng.Intn(6) - rng.Intn(6)
			bz += rng.Intn(6) - rng.Intn(6)
			walk = append(walk, [2]int{bx, bz})
			pos := srv.PlayerPosition("Watcher")
			dx, dy, dz := float64(bx)+0.5-pos.X, y-pos.Y, float64(bz)+0.5-pos.Z
			if dx*dx+dy*dy+dz*dz > 24*24 {
				t.Fatal("oracle walk left the player's exclusion radius")
			}
		}
	}
	wantWalk := [][2]int{{13, 3}, {11, 3}, {8, 3}, {11, 2}, {14, 4}, {9, 3}, {14, 5}}
	if !slices.Equal(walk, wantWalk) {
		t.Fatalf("vanilla pack walk = %v", walk)
	}

	// The remaining categories share the stream. Their untouched columns
	// end at y=63, giving 129 heights in [-64, 64]. These starts are all in
	// solid stone and reject without drawing a pack size or walking.
	wantStarts := [][3]int{{14, 53, 11}, {2, 17, 10}, {5, -1, 1}, {10, -2, 4}, {4, -38, 5}}
	for _, want := range wantStarts {
		bx, bz := rng.Intn(16), rng.Intn(16)
		by := -64 + rng.Intn(129)
		if got := [3]int{bx, by, bz}; got != want {
			t.Fatalf("later initial position = %v, want %v", got, want)
		}
	}
	srv.SpawnPass(seed)
	for range 4 {
		if got, want := srv.NextSpawnRandom(), rng.Int63(); got != want {
			t.Fatalf("RNG after vanilla pack walk = %d, want %d", got, want)
		}
	}
	if got := len(srv.Mobs()); got != 0 {
		t.Fatalf("%d mobs spawned inside the exclusion radius", got)
	}
}
