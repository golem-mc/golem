package redstone

import (
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
