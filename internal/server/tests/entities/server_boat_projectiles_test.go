package entities

// The projectile tests of the boats: a shaft or a thrown body meets a hull
// the way it meets any body, a boat being pickable for as long as it
// stands. An arrow's blow breaks an empty hull and spends the shaft; an
// egg's and a snowball's own is no damage, so the hull only rocks; and a
// creative shooter's blow leaves nothing behind.

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/entity"
)

// TestAnArrowDestroysABoatAndIsConsumed is the issue's repro: three hulls
// stand across the flight lines of two ownerless shafts, one behind the
// shooter and one each ahead. Each shaft meets its hull and breaks it — any
// arrow deals more than the forty a boat breaks past — and is spent on the
// hit, leaving the one hull the shots never crossed.
func TestAnArrowDestroysABoatAndIsConsumed(t *testing.T) {
	t.Parallel()
	srv, _, _ := servertest.BoatPlayer(t, "survival", "Archer", 0)
	for _, x := range []float64{10.5, 20.5, 30.5} {
		if srv.SpawnBoat(x, 64, 10.5, "minecraft:oak_boat") == 0 {
			t.Fatalf("the boat at x=%v never stood up", x)
		}
	}
	if id := srv.ShootArrow(14.5, 64.4, 10.5, 3, 0, 0); id == 0 {
		t.Fatal("the first arrow never flew")
	}
	if id := srv.ShootArrow(24.5, 64.4, 10.5, 3, 0, 0); id == 0 {
		t.Fatal("the second arrow never flew")
	}

	servertest.WaitForTicks(t, srv, 20, func() bool { return srv.BoatCount() == 1 },
		"the arrows never destroyed the two hulls their flights crossed")
	if got := srv.BoatCount(); got != 1 {
		t.Fatalf("boats standing = %d, want the two hit hulls destroyed", got)
	}
	if got := srv.ArrowCount(); got != 0 {
		t.Errorf("arrows standing = %d, want the two shots consumed by the hits", got)
	}
}

// TestAnEggHitsABoatForNoDamage throws an egg into a hull: the flight ends
// on it, the blow lays no damage — the egg's own nought — so the hull only
// rocks with the hit and stands, and the egg breaks on it.
// vanilla: VehicleEntity.hurtServer
func TestAnEggHitsABoatForNoDamage(t *testing.T) {
	t.Parallel()
	srv, _, connect := servertest.BoatPlayer(t, "survival", "Thrower", 0)
	watcher := connect().Join("Watcher")
	watcher.Settle()

	boatID := srv.SpawnBoat(4.5, 64, 5.5, "minecraft:oak_boat")
	if boatID == 0 {
		t.Fatal("the boat never stood up")
	}
	if egg := srv.ThrowThrown(entity.ThrownEgg, 4.5, 64.5, 2.5, 0, 0, 0, 7); egg == 0 {
		t.Fatal("the egg never flew")
	}

	fields := servertest.AwaitBoatMetadata(watcher, boatID)
	if got, ok := fields[int32(protocol.MetadataIndexVehicleShake)]; !ok || got != int32(10) {
		t.Errorf("boat shake ticks = %v (%v), want the ten the egg's hit laid", got, ok)
	}
	if got := srv.BoatCount(); got != 1 {
		t.Errorf("boats standing = %d, want the one the egg hit for no damage", got)
	}
	if thrown := srv.ThrownInDimension("minecraft:overworld"); len(thrown) != 0 {
		t.Errorf("%d thrown bodies still in the world, want the egg broken on the hull", len(thrown))
	}
}
