package mobs

import (
	"testing"

	"github.com/golem-mc/golem/internal/server"
)

func countKind(srv *server.Server, kind string) int {
	n := 0
	for _, m := range srv.Mobs() {
		if m.Kind == kind {
			n++
		}
	}
	return n
}

// spawnerCountAt noon over a stone floor: the spawner at (4, 64, 4) runs
// 600 ticks and the number of mobs of the kind it stood up is returned.
func spawnerCountAt(t *testing.T, kind string) int {
	t.Helper()
	srv, c := spawnerTests(t,
		`setblock 4 64 4 minecraft:spawner{Delay:0s,MinSpawnDelay:1s,MaxSpawnDelay:2s,SpawnData:{entity:{id:"`+kind+`"}}}`,
		"fill 0 63 0 8 63 8 minecraft:stone")
	srv.SetDayTime(noon)
	for range 600 {
		srv.TickBlockEntities()
	}
	c.Disconnect()
	return countKind(srv, kind)
}

// TestASilverfishSpawnerWorksInDaylight sets a silverfish spawner over stone
// at noon: the silverfish asks no darkness of its spawn, where the zombie
// beside it in the control run is refused all day.
// vanilla: Silverfish.checkSilverfishSpawnRules
func TestASilverfishSpawnerWorksInDaylight(t *testing.T) {
	t.Parallel()
	t.Run("silverfish", func(t *testing.T) {
		t.Parallel()
		if n := spawnerCountAt(t, silverfishKind); n == 0 {
			t.Error("no silverfish came out of the spawner at noon")
		}
	})
	t.Run("zombie", func(t *testing.T) {
		t.Parallel()
		if n := spawnerCountAt(t, "minecraft:zombie"); n != 0 {
			t.Errorf("%d zombies came out of the spawner at noon, want none", n)
		}
	})
}
