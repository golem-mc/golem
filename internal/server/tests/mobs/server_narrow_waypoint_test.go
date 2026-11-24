package mobs

import (
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// TestNarrowMobTurnsOffAWaypointDiagonally: a zombie starting off the centre
// of its cells on both axes banks each waypoint once inside the per-axis
// reach, so it rounds the end of a wall earlier than a circular reach would
// let it.
// vanilla: PathNavigation.followThePath
func TestNarrowMobTurnsOffAWaypointDiagonally(t *testing.T) {
	t.Parallel()
	cfg := servertest.MobTestConfig()
	cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
	srv, c := servertest.Join(t, cfg, "Fox")
	servertest.HoldTicks(t, srv)
	c.MoveToGround(0.5, 64, 0.5, true)
	c.Settle()
	srv.SetSpawning(false)
	srv.SetDayTime(18000)
	stone := servertest.MustState(t, "minecraft:stone", nil)
	for x := int32(-3); x <= 5; x++ {
		srv.SetBlock(x, 64, 3, stone)
		srv.SetBlock(x, 65, 3, stone)
	}
	zombie := srv.SpawnMobSeeded("minecraft:zombie", 6.9, 64, 5.9, 3)
	srv.SetMobTargetPlayer(zombie, "Fox")
	servertest.RunTicks(t, srv, 25)
	x, _, z, ok := srv.MobPos(zombie)
	if !ok {
		t.Fatal("the zombie is gone")
	}
	if z >= 3 {
		t.Fatalf("zombie at z = %.2f, want it round the wall onto the target's side (z < 3)", z)
	}
	// Measured at tick 25: 4.45 with the per-axis reach, 4.58 with a circle.
	if x > 4.52 {
		t.Errorf("zombie at x = %.2f after 25 ticks, want it past the wall end by x <= 4.52", x)
	}
}
