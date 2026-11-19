package mobs

import (
	"testing"
	"time"

	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// halloweenHeads spawns zombies on the given date and counts the pumpkin heads.
func halloweenHeads(t *testing.T, now time.Time) int {
	t.Helper()
	srv, c := servertest.Dial(t, servertest.BlockTestConfig(), servertest.NoTimeSync)
	srv.StopTick()
	srv.SetSpawnClock(func() time.Time { return now })
	c.Login("Actor")
	c.Configure()
	pumpkin, _ := registry.ItemID("minecraft:carved_pumpkin")
	lantern, _ := registry.ItemID("minecraft:jack_o_lantern")
	heads := 0
	for i := range 80 {
		id := srv.SpawnMob("minecraft:zombie", 2.5+float64(i%10)*3, 64, 2.5+float64(i/10)*3)
		if h := srv.MobHeadItem(id); h == pumpkin || h == lantern {
			heads++
		}
	}
	return heads
}

// TestZombiesSpawnWithPumpkinsOnHalloween drives the server's spawn door with
// the date set, so the clock the spawn reads is what decides.
func TestZombiesSpawnWithPumpkinsOnHalloween(t *testing.T) {
	t.Parallel()
	if n := halloweenHeads(t, time.Date(2026, time.October, 31, 12, 0, 0, 0, time.UTC)); n == 0 {
		t.Error("no pumpkin heads among 80 zombies on October 31")
	}
}

func TestZombiesSpawnBareHeadedOffHalloween(t *testing.T) {
	t.Parallel()
	if n := halloweenHeads(t, time.Date(2026, time.October, 30, 12, 0, 0, 0, time.UTC)); n != 0 {
		t.Errorf("%d pumpkin heads among 80 zombies on October 30", n)
	}
}
