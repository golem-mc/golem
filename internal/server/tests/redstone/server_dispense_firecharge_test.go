package redstone

import (
	"math"
	"slices"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// TestADispensedFireChargeFliesAsASmallFireball checks the charge leaves as a
// small fireball and sets fire to the air in front of the wall it strikes.
// vanilla: FireChargeItem.asProjectile
func TestADispensedFireChargeFliesAsASmallFireball(t *testing.T) {
	t.Parallel()
	srv, w := servertest.Dispensing(t)
	srv.SetBlock(4, 64, 6, servertest.BlockState(t, "minecraft:stone"))

	if !srv.SetInventoryAt(4, 64, 0, 0, servertest.ItemStack(t, "minecraft:fire_charge", 2)) {
		t.Fatal("the dispenser has no block entity to fill")
	}
	srv.Dispense(4, 64, 0, protocol.FaceSouth)
	w.ExpectSpawned(protocol.EntityTypeSmallFireball)
	servertest.DriveTargetTicks(srv, 60)

	if name, _ := servertest.StateProps(srv.BlockAt(4, 64, 5)); name != "minecraft:fire" {
		t.Errorf("block in front of the wall = %q, want minecraft:fire", name)
	}
	if got := srv.InventoryAt(4, 64, 0)[0].Count; got != 1 {
		t.Errorf("the dispenser keeps %d charges, want 1", got)
	}
}

// TestADispensedFireChargeLeavesAtFullSpeedAlongTheFace checks the shot
// speed is the item's power of 1.0 rather than a fireball's 0.1, for the
// horizontal and the vertical faces, and that event 1018 plays.
// vanilla: FireChargeItem.createDispenseConfig, Projectile.shoot
func TestADispensedFireChargeLeavesAtFullSpeedAlongTheFace(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name       string
		face       int32
		wx, wy, wz float64
	}{
		{"south", protocol.FaceSouth, 0, 0, 1},
		{"up", protocol.FaceTop, 0, 1, 0},
		{"down", protocol.FaceBottom, 0, -1, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv, w := servertest.Dispensing(t)
			if !srv.SetInventoryAt(4, 64, 0, 0, servertest.ItemStack(t, "minecraft:fire_charge", 1)) {
				t.Fatal("the dispenser has no block entity to fill")
			}
			srv.Dispense(4, 64, 0, tc.face)
			seen := w.ExpectSpawned(protocol.EntityTypeSmallFireball)
			w.DrainRecorded()

			speed := math.Sqrt(seen.Vx*seen.Vx + seen.Vy*seen.Vy + seen.Vz*seen.Vz)
			if math.Abs(speed-1) > 0.2 {
				t.Errorf("the charge leaves at %g blocks a tick, want about 1", speed)
			}
			if seen.Vx*tc.wx+seen.Vy*tc.wy+seen.Vz*tc.wz < 0.9*speed {
				t.Errorf("the charge flies (%g, %g, %g), want along (%g, %g, %g)", seen.Vx, seen.Vy, seen.Vz, tc.wx, tc.wy, tc.wz)
			}
			if !slices.Contains(w.Events, 1018) {
				t.Errorf("events %v lack the fire charge dispense event 1018", w.Events)
			}
		})
	}
}

// TestADispensedFireChargeLightsTheCampfireItHits checks the burning charge
// lights an unlit campfire through its onProjectileHit hook. The charge is
// shot down onto it: one shot south leaves at y 64.5 and passes over the
// 7/16-tall campfire.
// vanilla: AbstractHurtingProjectile.shouldBurn, CampfireBlock.onProjectileHit
func TestADispensedFireChargeLightsTheCampfireItHits(t *testing.T) {
	t.Parallel()
	srv, w := servertest.Dispensing(t)
	srv.SetBlock(4, 63, 0, servertest.MustState(t, "minecraft:campfire", map[string]string{"lit": "false"}))

	if !srv.SetInventoryAt(4, 64, 0, 0, servertest.ItemStack(t, "minecraft:fire_charge", 1)) {
		t.Fatal("the dispenser has no block entity to fill")
	}
	srv.Dispense(4, 64, 0, protocol.FaceBottom)
	w.ExpectSpawned(protocol.EntityTypeSmallFireball)
	servertest.DriveTargetTicks(srv, 60)

	servertest.ExpectBlockNamed(t, srv, 4, 63, 0, "minecraft:campfire", map[string]string{"lit": "true"})
}

// TestADispensedFireChargePrimesTheTNTItHits checks the burning charge primes
// the TNT it strikes rather than leaving it to the fire it places.
// vanilla: AbstractHurtingProjectile.shouldBurn, TntBlock.onProjectileHit
func TestADispensedFireChargePrimesTheTNTItHits(t *testing.T) {
	t.Parallel()
	srv, w := servertest.Dispensing(t)
	srv.SetBlock(4, 64, 6, servertest.BlockState(t, "minecraft:tnt"))

	if !srv.SetInventoryAt(4, 64, 0, 0, servertest.ItemStack(t, "minecraft:fire_charge", 1)) {
		t.Fatal("the dispenser has no block entity to fill")
	}
	srv.Dispense(4, 64, 0, protocol.FaceSouth)
	w.ExpectSpawned(protocol.EntityTypeSmallFireball)
	servertest.DriveTargetTicks(srv, 60)
	w.ExpectSpawned(protocol.EntityTypeTnt)

	if name, _ := servertest.StateProps(srv.BlockAt(4, 64, 6)); name != "minecraft:air" {
		t.Errorf("block hit = %q, want minecraft:air", name)
	}
}
