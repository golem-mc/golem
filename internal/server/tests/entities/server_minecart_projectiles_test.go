package entities

// A shaft or a thrown body meets a minecart as it meets any body, a cart
// being pickable for as long as it stands: an arrow breaks it and leaves
// the cart item, a snowball only rocks it.

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"
)

// vanilla: AbstractMinecart.isPickable, VehicleEntity.hurtServer
func TestAnArrowBreaksAMinecartAndLeavesItsItem(t *testing.T) {
	t.Parallel()
	srv, _, _ := cartPlayer(t, "survival", "Archer", 0)
	if srv.SpawnMinecart(4.5, 64, 5.5) == 0 {
		t.Fatal("the cart never stood up")
	}
	if id := srv.ShootArrow(4.5, 64.4, 2.5, 0, 0, 3); id == 0 {
		t.Fatal("the arrow never flew")
	}

	servertest.WaitForTicks(t, srv, 20, func() bool {
		return len(srv.MinecartsInDimension("minecraft:overworld")) == 0 &&
			servertest.BlastLeftItem(srv.ItemsInDimension("minecraft:overworld"), "minecraft:minecart")
	}, "the arrow never broke the cart and laid its item")
	if got := srv.ArrowCount(); got != 0 {
		t.Errorf("arrows standing = %d, want the shaft consumed by the hit", got)
	}
}
