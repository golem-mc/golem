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

// TestABodyABoatCannotSeatPushesIt: an iron golem, too wide for a seat,
// standing against an empty boat's east side pushes the boat west and is
// pushed east.
// vanilla: AbstractBoat.tick, Entity.push, LivingEntity.pushEntities
func TestABodyABoatCannotSeatPushesIt(t *testing.T) {
	t.Parallel()
	srv := boatPool(t, "Bargee")
	boat := srv.SpawnBoat(8.5, 63+8.0/9.0, 7.5, "minecraft:oak_boat")
	golem := srv.SpawnMob("minecraft:iron_golem", 9.5, 63, 7.5)
	if boat == 0 || golem == 0 {
		t.Fatal("spawn came to nothing")
	}
	servertest.RunTicks(t, srv, 20)
	if got := srv.BoatPassengerIDs(boat); len(got) != 0 {
		t.Fatalf("the boat seated %v, want nobody", got)
	}
	if x := boatX(t, srv, boat); x > 8.5-0.1 {
		t.Errorf("after 20 ticks the boat stands at x=%v, want it pushed a tenth or more west of 8.5", x)
	}
	if x, _, _, ok := srv.MobPos(golem); !ok || x < 9.5+0.1 {
		t.Errorf("after 20 ticks the golem stands at x=%v (%v), want it pushed a tenth or more east of 9.5", x, ok)
	}
}

// TestAFullBoatPushesAPigAndStays: a boat whose two seats are taken pushes
// a pig at its side away, and is not pushed itself: Entity.push moves no
// vehicle.
// vanilla: Entity.push
func TestAFullBoatPushesAPigAndStays(t *testing.T) {
	t.Parallel()
	srv := boatPool(t, "Ferrywoman")
	boat := srv.SpawnBoat(8.5, 63+8.0/9.0, 7.5, "minecraft:oak_boat")
	if boat == 0 {
		t.Fatal("SpawnBoat came to nothing")
	}
	for range 2 {
		if srv.SpawnMob("minecraft:pig", 8.5, 63+8.0/9.0, 7.5) == 0 {
			t.Fatal("SpawnMob came to nothing")
		}
	}
	servertest.RunTicks(t, srv, 2)
	if got := srv.BoatPassengerIDs(boat); len(got) != 2 {
		t.Fatalf("the boat seats %v, want both pigs", got)
	}
	startX := boatX(t, srv, boat)
	pig := srv.SpawnMob("minecraft:pig", 9.3, 63+8.0/9.0, 7.5)
	if pig == 0 {
		t.Fatal("SpawnMob came to nothing")
	}
	servertest.RunTicks(t, srv, 20)
	if x := boatX(t, srv, boat); x < startX-0.01 || x > startX+0.01 {
		t.Errorf("after 20 ticks the full boat stands at x=%v, want it still at %v", x, startX)
	}
	if x, _, _, ok := srv.MobPos(pig); !ok || x < 9.3+0.2 {
		t.Errorf("after 20 ticks the pig stands at x=%v (%v), want it pushed east of 9.3 by two tenths or more", x, ok)
	}
}
