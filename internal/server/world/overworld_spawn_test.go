package world

import "testing"

func TestFindSpawnMatchesVanilla(t *testing.T) {
	t.Parallel()
	// Captured from level.dat's Data.spawn.pos after running the vanilla 26.3
	// reference server once per seed, with level-type=minecraft:normal and
	// level-seed=0 / 12345 in fresh scratch server.properties (reference:setup).
	for _, tc := range []struct {
		seed int64
		want SpawnPos
	}{{0, SpawnPos{X: -32, Y: 69, Z: 0}}, {12345, SpawnPos{X: 96, Y: 145, Z: -32}}} {
		if got := NewOverworldGenerator(tc.seed).FindSpawn(); got != tc.want {
			t.Errorf("seed %d: FindSpawn() = %v, want vanilla %v", tc.seed, got, tc.want)
		}
	}
}

func TestInitialSpawnSpiral(t *testing.T) {
	var chunks []ChunkPos
	seen := map[ChunkPos]bool{}
	got := findSpawnNear(-2, 3, func(cx, cz int32) (SpawnPos, bool) {
		pos := ChunkPos{X: cx, Z: cz}
		if seen[pos] || cx < -7 || cx > 3 || cz < -2 || cz > 8 {
			t.Errorf("repeated or out-of-range chunk %v", pos)
		}
		seen[pos] = true
		chunks = append(chunks, pos)
		return SpawnPos{}, false
	})
	if want := (SpawnPos{X: -24, Y: 64, Z: 56}); got != want {
		t.Errorf("fallback = %v, want %v", got, want)
	}
	if len(seen) != 121 {
		t.Fatalf("scanned %d distinct chunks, want 121", len(seen))
	}
	// First ring: centre, east, southeast, south, southwest, west,
	// northwest, north, northeast; the second ring starts to the east.
	for i, offset := range [][2]int32{{0, 0}, {1, 0}, {1, 1}, {0, 1}, {-1, 1}, {-1, 0}, {-1, -1}, {0, -1}, {1, -1}, {2, -1}} {
		if want := (ChunkPos{X: -2 + offset[0], Z: 3 + offset[1]}); chunks[i] != want {
			t.Errorf("chunk %d = %v, want %v", i, chunks[i], want)
		}
	}
	calls := 0
	want := SpawnPos{X: 16, Y: 70, Z: 1}
	got = findSpawnNear(0, 0, func(int32, int32) (SpawnPos, bool) {
		calls++
		return want, calls == 2
	})
	if calls != 2 || got != want {
		t.Errorf("early success = %v after %d calls, want %v after 2", got, calls, want)
	}
}
