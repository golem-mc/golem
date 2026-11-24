package mobs

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"
)

// TestNarrowMobTurnsOffAWaypointDiagonally: a zombie starting off the centre
// of its cells on both axes banks each waypoint once inside the per-axis
// reach, so it rounds the end of a wall earlier than a circular reach would
// let it.
// vanilla: PathNavigation.followThePath
func TestNarrowMobTurnsOffAWaypointDiagonally(t *testing.T) {
	t.Parallel()
	srv := foxScene(t, false)
	srv.SetSpawning(false)
	stone := servertest.MustState(t, "minecraft:stone", nil)
	for x := int32(-3); x <= 5; x++ {
		srv.SetBlock(x, 64, 3, stone)
		srv.SetBlock(x, 65, 3, stone)
	}
	zombie := srv.SpawnMobSeeded("minecraft:zombie", 6.9, 64, 5.9, 3)
	srv.SetMobTargetPlayer(zombie, "Fox")
	servertest.RunTicks(t, srv, 25)
	x, _, _, _ := srv.MobPos(zombie)
	// Measured at tick 25: 4.45 with the per-axis reach, 4.58 with a circle.
	if x > 4.52 {
		t.Errorf("zombie at x = %.2f after 25 ticks, want it past the wall end by x <= 4.52", x)
	}
}
