package movement

// A projectile that has not yet left its shooter passes over everyone who
// shares the shooter's root vehicle, not just the shooter's own body:
// the other rider and the hull itself.
// vanilla: Projectile.canHitEntity, Projectile.isOutsideOwnerCollisionRange

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
)

// sharedBoat seats two survival players in one boat and returns the server,
// the shooter's client, the boat and the shooter's and the mate's seats.
func sharedBoat(t *testing.T) (srv *server.Server, shooter *servertest.Client, boatID int32, from, to [3]float64) {
	t.Helper()
	srv, shooter, connect := servertest.BoatPlayer(t, "survival", "Shooter", 0)
	boatID = srv.SpawnBoat(1.5, 64, 0.5, "minecraft:oak_boat")
	if boatID == 0 {
		t.Fatal("the boat never stood up")
	}
	shooter.UseOn(boatID)
	servertest.AwaitPassengersFor(shooter, boatID)
	shooter.Settle()
	mate := connect().Join("Mate")
	mate.MoveToGround(1.5, 64, 0.5, true)
	mate.Settle()
	mate.UseOn(boatID)
	// The settle, not the passenger count: the count grows before the
	// boarding moves both riders to their seats.
	mate.Settle()
	if got := len(srv.BoatPassengerIDs(boatID)); got != 2 {
		t.Fatalf("the boat carries %d riders, want 2", got)
	}
	a, b := srv.PlayerPosition("Shooter"), srv.PlayerPosition("Mate")
	return srv, shooter, boatID, [3]float64{a.X, a.Y, a.Z}, [3]float64{b.X, b.Y, b.Z}
}

// TestAnArrowPassesTheBoatCoRiderUntilItClearsTheGroup shoots point-blank
// from one seat at the other: the shaft crosses the co-rider and the hull
// on its first tick unharmed, then flies on.
func TestAnArrowPassesTheBoatCoRiderUntilItClearsTheGroup(t *testing.T) {
	t.Parallel()
	srv, _, _, from, to := sharedBoat(t)
	dx, dz := to[0]-from[0], to[2]-from[2]
	n := math.Hypot(dx, dz)
	if n == 0 {
		t.Fatal("the riders share one spot")
	}
	if id := srv.ShootPlayerArrow("Shooter", from[0], from[1]+1, from[2], dx/n*1.5, 0, dz/n*1.5); id == 0 {
		t.Fatal("the arrow never flew")
	}
	servertest.AwaitTicks(t, srv, 3)
	if got := srv.PlayerHealth("Mate"); got != 20 {
		t.Errorf("the co-rider's health = %v, want the arrow passed over them", got)
	}
	if got := srv.BoatCount(); got != 1 {
		t.Errorf("boats standing = %d, want the shared hull passed over", got)
	}
	if got := srv.ArrowCount(); got != 1 {
		t.Errorf("arrows flying = %d, want the shaft spent on nothing", got)
	}
}

// TestARocketPassesTheBoatCoRiderUntilItClearsTheGroup lights a rocket
// under the co-rider's feet: it rises through them and the hull and
// bursts only when its life ends.
func TestARocketPassesTheBoatCoRiderUntilItClearsTheGroup(t *testing.T) {
	t.Parallel()
	srv, _, _, _, to := sharedBoat(t)
	servertest.HoldTicks(t, srv)
	if id := srv.SpawnPlayerFirework("Shooter", to[0], to[1]-0.2, to[2]); id == 0 {
		t.Fatal("the rocket never flew")
	}
	servertest.AwaitTicks(t, srv, 6)
	if got := srv.FireworkCount(); got != 1 {
		t.Errorf("rockets flying = %d, want the rocket still rising past the co-rider", got)
	}
}

// TestAHookCastAtTheBoatCoRiderCatchesNothing casts the rod at the other
// seat: the hook starts among the group and hooks none of it.
func TestAHookCastAtTheBoatCoRiderCatchesNothing(t *testing.T) {
	t.Parallel()
	srv, c, _, from, to := sharedBoat(t)
	srv.GiveSlot("Shooter", 36, protocol.ItemStack{ItemID: servertest.MustItem("minecraft:fishing_rod"), Count: 1})
	yaw := float32(-math.Atan2(to[0]-from[0], to[2]-from[2]) * 180 / math.Pi)
	servertest.UseHeld(c, yaw, 0)
	_, spawns, metas, _, _, _ := servertest.FishingRead(c, srv, 40)
	if len(servertest.SpawnsOf(spawns, protocol.EntityTypeFishingBobber)) != 1 {
		t.Fatal("the cast never spawned a bobber")
	}
	for _, m := range metas {
		if m.HookedTold && m.Hooked != 0 {
			t.Errorf("the hook caught body %d, want the group passed over", m.Hooked)
		}
	}
}
