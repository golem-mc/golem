package entities

import (
	"testing"

	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// boatPool holds srv's loop and floods a still one-deep pool, x 5..12 and
// z 4..11 at y=63, clear of the joined player at the origin.
func boatPool(t *testing.T, name string) *server.Server {
	t.Helper()
	srv, _, _ := servertest.BoatPlayer(t, "creative", name, 0)
	servertest.HoldTicks(t, srv)
	water := servertest.MustState(t, "minecraft:water", nil)
	for x := int32(5); x <= 12; x++ {
		for z := int32(4); z <= 11; z++ {
			srv.SetBlock(x, 63, z, water)
		}
	}
	return srv
}

// boatX is the boat's x, failing the test if it is gone.
func boatX(t *testing.T, srv *server.Server, id int32) float64 {
	t.Helper()
	x, _, _, _, _, ok := srv.BoatState(id)
	if !ok {
		t.Fatalf("boat %d is gone", id)
	}
	return x
}

// TestTwoOverlappingBoatsPushEachOtherApart: two empty boats on still
// water 0.3 apart on X each take the other's push and drift apart.
// vanilla: AbstractBoat.tick, AbstractBoat.push
func TestTwoOverlappingBoatsPushEachOtherApart(t *testing.T) {
	t.Parallel()
	srv := boatPool(t, "Raftsman")
	west := srv.SpawnBoat(8.35, 63+8.0/9.0, 7.5, "minecraft:oak_boat")
	east := srv.SpawnBoat(8.65, 63+8.0/9.0, 7.5, "minecraft:oak_boat")
	if west == 0 || east == 0 {
		t.Fatal("SpawnBoat came to nothing")
	}
	servertest.RunTicks(t, srv, 20)
	wx, ex := boatX(t, srv, west), boatX(t, srv, east)
	if wx > 8.35-0.1 || ex < 8.65+0.1 {
		t.Errorf("after 20 ticks the boats stand at x=%v and x=%v, want each pushed a tenth or more away from the other (from 8.35 and 8.65)", wx, ex)
	}
}
