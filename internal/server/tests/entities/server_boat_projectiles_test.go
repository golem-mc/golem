package entities

// The projectile tests of the boats: a shaft or a thrown body meets a hull
// the way it meets any body, a boat being pickable for as long as it
// stands. An arrow's blow breaks an empty hull and spends the shaft; an
// egg's and a snowball's own is no damage, so the hull only rocks; and a
// creative shooter's blow leaves nothing behind.

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/registry"
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

// TestAnArrowDestroysAChestBoatAndLeavesItsItem is the same breaking blow
// on a chest boat: the hull breaks and the wreck's own chest boat item is
// laid where it stood.
// vanilla: VehicleEntity.hurtServer, AbstractBoat.destroy
func TestAnArrowDestroysAChestBoatAndLeavesItsItem(t *testing.T) {
	t.Parallel()
	srv, _, _ := servertest.BoatPlayer(t, "survival", "Archer", 0)
	if srv.SpawnBoat(4.5, 64, 5.5, "minecraft:oak_chest_boat") == 0 {
		t.Fatal("the chest boat never stood up")
	}
	if id := srv.ShootArrow(4.5, 64.4, 2.5, 0, 0, 3); id == 0 {
		t.Fatal("the arrow never flew")
	}

	servertest.WaitForTicks(t, srv, 20, func() bool {
		return srv.BoatCount() == 0 &&
			servertest.BlastLeftItem(srv.ItemsInDimension("minecraft:overworld"), "minecraft:oak_chest_boat")
	}, "the arrow never broke the chest boat and laid its item")
	if got := srv.ArrowCount(); got != 0 {
		t.Errorf("arrows standing = %d, want the shaft consumed by the hit", got)
	}
}

// TestACreativePlayersArrowDestroysABoatWithoutDropping shoots the archer's
// own shaft into a hull: the blow breaks it the same, while the discard of
// it skips the boat item for a shooter whose infinite materials leave
// nothing behind.
// vanilla: VehicleEntity.hurtServer
func TestACreativePlayersArrowDestroysABoatWithoutDropping(t *testing.T) {
	t.Parallel()
	srv, _, _ := servertest.BoatPlayer(t, "creative", "Archer", 0)
	if srv.SpawnBoat(4.5, 64, 5.5, "minecraft:oak_boat") == 0 {
		t.Fatal("the boat never stood up")
	}
	if id := srv.ShootPlayerArrow("Archer", 4.5, 64.4, 2.5, 0, 0, 3); id == 0 {
		t.Fatal("the archer's arrow never flew")
	}

	servertest.WaitForTicks(t, srv, 20, func() bool { return srv.BoatCount() == 0 },
		"the creative archer's arrow never destroyed the hull")
	if got := srv.ItemCount(); got != 0 {
		t.Errorf("items standing = %d, want none: a creative shot leaves nothing", got)
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

// TestASnowballHitsABoatForNoDamage is TestAnEggHitsABoatForNoDamage with
// the thrown body a snowball: the same nought against a hull that is no
// blaze, the same rocking and the same standing.
// vanilla: Snowball.onHitEntity, VehicleEntity.hurtServer
func TestASnowballHitsABoatForNoDamage(t *testing.T) {
	t.Parallel()
	srv, _, connect := servertest.BoatPlayer(t, "survival", "Thrower", 0)
	watcher := connect().Join("Watcher")
	watcher.Settle()

	boatID := srv.SpawnBoat(4.5, 64, 5.5, "minecraft:oak_boat")
	if boatID == 0 {
		t.Fatal("the boat never stood up")
	}
	if ball := srv.ThrowThrown(entity.ThrownSnowball, 4.5, 64.5, 2.5, 0, 0, 0, 7); ball == 0 {
		t.Fatal("the snowball never flew")
	}

	fields := servertest.AwaitBoatMetadata(watcher, boatID)
	if got, ok := fields[int32(protocol.MetadataIndexVehicleShake)]; !ok || got != int32(10) {
		t.Errorf("boat shake ticks = %v (%v), want the ten the snowball's hit laid", got, ok)
	}
	if got := srv.BoatCount(); got != 1 {
		t.Errorf("boats standing = %d, want the one the snowball hit for no damage", got)
	}
	if thrown := srv.ThrownInDimension("minecraft:overworld"); len(thrown) != 0 {
		t.Errorf("%d thrown bodies still in the world, want the snowball broken on the hull", len(thrown))
	}
}

// TestAFlightFromABoatLeavesItsOwnHullStanding is the review's repro: a
// rider shoots an arrow steeply down from the seat. The shaft cannot meet
// the hull it flies from, so it sticks in the ground under the boat and the
// hull stands.
// vanilla: Projectile.canHitEntity, Projectile.checkLeftOwner
func TestAFlightFromABoatLeavesItsOwnHullStanding(t *testing.T) {
	t.Parallel()
	srv, c, _ := servertest.BoatPlayer(t, "survival", "Rower", 0)
	boatID := srv.SpawnBoat(1.5, 64, 0.5, "minecraft:oak_boat")
	if boatID == 0 {
		t.Fatal("the boat never stood up")
	}
	c.UseOn(boatID)
	if riders := servertest.AwaitPassengersFor(c, boatID); len(riders) != 1 || riders[0] != c.PlayerEntityID {
		t.Fatalf("the rower never boarded: passengers = %v", riders)
	}
	pos := srv.PlayerPosition("Rower")
	if id := srv.ShootPlayerArrow("Rower", pos.X, pos.Y+1.6, pos.Z, 0, -3, 0); id == 0 {
		t.Fatal("the rower's arrow never flew")
	}

	servertest.AwaitTicks(t, srv, 20)
	if got := srv.BoatCount(); got != 1 {
		t.Errorf("boats standing = %d, want the rower's own hull left standing", got)
	}
	if got := srv.ArrowCount(); got != 1 {
		t.Errorf("arrows standing = %d, want the shaft stuck in the ground under the hull", got)
	}
}

// TestAThrowFromABoatLeavesItsOwnHullStanding is the same repro for a
// thrown body: an egg thrown straight down from the seat cannot rock the
// hull it was thrown from, and breaks on the ground instead.
// vanilla: Projectile.canHitEntity, Projectile.checkLeftOwner
func TestAThrowFromABoatLeavesItsOwnHullStanding(t *testing.T) {
	t.Parallel()
	srv, c, connect := servertest.BoatPlayer(t, "survival", "Thrower", 0)
	watcher := connect().Join("Watcher")
	watcher.Settle()

	boatID := srv.SpawnBoat(1.5, 64, 0.5, "minecraft:oak_boat")
	if boatID == 0 {
		t.Fatal("the boat never stood up")
	}
	c.UseOn(boatID)
	if riders := servertest.AwaitPassengersFor(c, boatID); len(riders) != 1 || riders[0] != c.PlayerEntityID {
		t.Fatalf("the thrower never boarded: passengers = %v", riders)
	}
	watcher.Settle()
	pos := srv.PlayerPosition("Thrower")
	if egg := srv.ThrowThrown(entity.ThrownEgg, pos.X, pos.Y+1.6, pos.Z, 0, 90, c.PlayerEntityID, 7); egg == 0 {
		t.Fatal("the egg never flew")
	}

	rocked := watcher.PumpTicks(srv, 5, func(id int32, data []byte) bool {
		if id != protocol.IDPlayEntityMetadata {
			return false
		}
		return protocol.NewReader(data).ReadVarInt() == boatID
	})
	if rocked {
		t.Error("the thrower's own hull was rocked by the throw")
	}
	if got := srv.BoatCount(); got != 1 {
		t.Errorf("boats standing = %d, want the thrower's own hull left standing", got)
	}
}

// TestAnArrowHitOnABoatIsHeard: the shaft's blow on a hull plays the
// arrow's hit voice from the hull, not the voice of a shaft that flew
// through and stuck in the ground beyond it.
// vanilla: AbstractArrow.onHitEntity
func TestAnArrowHitOnABoatIsHeard(t *testing.T) {
	t.Parallel()
	srv, _, connect := servertest.BoatPlayer(t, "survival", "Shooter", 0)
	watcher := connect().Join("Watcher")
	watcher.Settle()

	if srv.SpawnBoat(4.5, 64, 5.5, "minecraft:oak_boat") == 0 {
		t.Fatal("the boat never stood up")
	}
	if id := srv.ShootArrow(4.5, 64.4, 2.5, 0, 0, 3); id == 0 {
		t.Fatal("the arrow never flew")
	}

	soundID, ok := registry.SoundEventID("minecraft:entity.arrow.hit")
	if !ok {
		t.Fatal("the sound registry has no arrow hit")
	}
	// A shaft that flew past the hull would sound where the ground
	// stopped it, far down the flight line and outside the hull's own
	// column.
	heard := watcher.ReadTicksSkipping(srv, 200, nil, func(id int32, data []byte) bool {
		if id != protocol.IDPlaySoundEffect {
			return false
		}
		r := protocol.NewReader(data)
		if r.ReadVarInt() != soundID+1 {
			return false
		}
		r.ReadVarInt() // sound category
		x := float64(r.ReadI32()) / 8
		y := float64(r.ReadI32()) / 8
		z := float64(r.ReadI32()) / 8
		return x >= 3.5 && x <= 5.5 && y >= 63.5 && y <= 65 && z >= 4.0 && z <= 7.0
	})
	if !heard {
		t.Error("the arrow's hit on the boat was never heard from the hull")
	}
	if got := srv.BoatCount(); got != 0 {
		t.Errorf("boats standing = %d, want the hull the arrow broke", got)
	}
}

// TestTwoArrowsInOneTickBreakBothBoats: two shafts fired together meet the
// first hull in the same tick. The first breaks it; the second flies
// through the hull it can no longer meet and breaks the one behind.
// vanilla: Projectile.onHit, Entity.discard
func TestTwoArrowsInOneTickBreakBothBoats(t *testing.T) {
	t.Parallel()
	srv, _, _ := servertest.BoatPlayer(t, "survival", "Archer", 0)
	for _, x := range []float64{20.5, 22.5} {
		if srv.SpawnBoat(x, 64, 10.5, "minecraft:oak_boat") == 0 {
			t.Fatalf("the boat at x=%v never stood up", x)
		}
	}
	for i := range 2 {
		if id := srv.ShootArrow(18.5, 64.4, 10.5, 3, 0, 0); id == 0 {
			t.Fatalf("arrow %d never flew", i+1)
		}
	}
	servertest.WaitForTicks(t, srv, 20, func() bool { return srv.BoatCount() == 0 },
		"the arrows never broke both hulls")
	if got := srv.ArrowCount(); got != 0 {
		t.Errorf("arrows standing = %d, want both spent on the hulls", got)
	}
}
