package entity

import (
	"math/rand"
	"testing"
)

type countedSpawnPointWorld struct {
	spawnTestWorld
	reads int
}

func (w *countedSpawnPointWorld) SpawnPoint() (int32, int32, int32, bool) {
	w.reads++
	return w.spawnTestWorld.SpawnPoint()
}

func TestNaturalSpawnReadsSpawnPointOnce(t *testing.T) {
	w := &countedSpawnPointWorld{spawnTestWorld: newSpawnTestWorld()}
	players := []SpawnPlayer{{EntityID: 1, X: 80, Y: 30}}
	for range 2 {
		w.reads = 0
		SpawnNaturally(w, rand.New(rand.NewSource(1)), []ChunkPos{{}, {X: 1}, {Z: 1}}, players, nil,
			&SpawnPotential{}, []SpawnPass{MonsterPass(&SpawnCounts{}), AmbientPass(&SpawnCounts{})},
			289, func(SpawnRequest) { t.Fatal("spawned without ground") })
		if w.reads != 1 {
			t.Fatalf("spawn point read %d times in one pass, want 1", w.reads)
		}
	}
}

func TestNaturalSpawnDistanceUsesSnapshot(t *testing.T) {
	for _, tc := range []struct {
		name       string
		dx, dy, dz int32
		guarded    bool
		distance   float64
		loaded     bool
		inBorder   bool
		want       bool
	}{
		{"at spawn", 0, 0, 0, true, 10000, true, true, false},
		{"inside x radius", 23, 0, 0, true, 10000, true, true, false},
		{"outside x radius", 24, 0, 0, true, 10000, true, true, true},
		{"inside z radius", 0, 0, -23, true, 10000, true, true, false},
		{"outside z radius", 0, 0, -24, true, 10000, true, true, true},
		{"inside above centre", 0, 24, 0, true, 10000, true, true, false},
		{"outside below centre", 0, -24, 0, true, 10000, true, true, true},
		{"other dimension", 0, 0, 0, false, 10000, true, true, true},
		{"player at 24 blocks", 24, 0, 0, true, 576, true, true, false},
		{"player beyond 24 blocks", 24, 0, 0, true, 577, true, true, true},
		{"unloaded landing", 24, 0, 0, true, 10000, false, true, false},
		{"outside border", 24, 0, 0, true, 10000, true, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := &countedSpawnPointWorld{spawnTestWorld: newSpawnTestWorld()}
			w.loaded = tc.loaded
			w.inBorder = func(_, _ int32) bool { return tc.inBorder }
			spawn := spawnPoint{x: 37, y: 70, z: -21, guarded: tc.guarded}
			got := rightDistanceToPlayerAndSpawnPoint(w, spawn, spawn.x+tc.dx, spawn.y+tc.dy, spawn.z+tc.dz, tc.distance)
			if got != tc.want {
				t.Errorf("distance gate = %v, want %v", got, tc.want)
			}
			if w.reads != 0 {
				t.Errorf("candidate reread the spawn point %d times", w.reads)
			}
		})
	}
}
