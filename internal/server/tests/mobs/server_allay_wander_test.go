package mobs

import (
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"
)

// vanilla: AllayAi.initIdleActivity, RandomStroll.strollFlyOrSwim
func TestAnAllayWandersAfterIdleTicksWithOnlyASpectator(t *testing.T) {
	t.Parallel()
	cfg := servertest.MobTestConfig()
	cfg.GameMode = "spectator"
	srv, c := servertest.Join(t, cfg, "Watcher")
	servertest.HoldTicks(t, srv)
	id := srv.SpawnMobSeeded(allay, 2.5, 80, 0.5, 1)
	c.ExpectMobSpawn(allayType())
	// Keep losing the stroll roll until noActionTime passes 100, so a
	// surviving earlier flight cannot hide a refusal to choose a new one.
	for range 120 {
		srv.SeedMob(id, 1)
		servertest.RunTicks(t, srv, 1)
	}
	x, y, z, ok := srv.MobPos(id)
	if !ok || math.Abs(x-2.5)+math.Abs(y-80)+math.Abs(z-0.5) > 0.01 {
		t.Fatalf("allay moved before the idle interval ended: (%g, %g, %g), present = %v", x, y, z, ok)
	}
	srv.SeedMob(id, 2)
	servertest.UntilTicks(t, srv, 200, "the idle allay choosing a new wandering destination", func() bool {
		nx, ny, nz, present := srv.MobPos(id)
		return present && math.Hypot(math.Hypot(nx-x, nz-z), ny-y) > 1
	})
}
