package entities

// A shaft or a thrown body meets a minecart as it meets any body, a cart
// being pickable for as long as it stands: an arrow breaks it and leaves
// the cart item, a snowball only rocks it.

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/entity"
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

// vanilla: VehicleEntity.hurtServer
func TestACreativePlayersArrowBreaksAMinecartWithoutDropping(t *testing.T) {
	t.Parallel()
	srv, _, _ := cartPlayer(t, "creative", "Archer", 0)
	if srv.SpawnMinecart(4.5, 64, 5.5) == 0 {
		t.Fatal("the cart never stood up")
	}
	if id := srv.ShootPlayerArrow("Archer", 4.5, 64.4, 2.5, 0, 0, 3); id == 0 {
		t.Fatal("the arrow never flew")
	}

	servertest.WaitForTicks(t, srv, 20, func() bool {
		return len(srv.MinecartsInDimension("minecraft:overworld")) == 0
	}, "the creative archer's arrow never broke the cart")
	if got := srv.ItemCount(); got != 0 {
		t.Errorf("items standing = %d, want none: a creative shot leaves nothing", got)
	}
}

// vanilla: Snowball.onHitEntity, VehicleEntity.hurtServer
func TestASnowballRocksAMinecartForNoDamage(t *testing.T) {
	t.Parallel()
	srv, _, connect := cartPlayer(t, "survival", "Thrower", 0)
	watcher := connect().Join("Watcher")
	watcher.Settle()

	cartID := srv.SpawnMinecart(4.5, 64, 5.5)
	if cartID == 0 {
		t.Fatal("the cart never stood up")
	}
	if ball := srv.ThrowThrown(entity.ThrownSnowball, 4.5, 64.5, 2.5, 0, 0, 0, 7); ball == 0 {
		t.Fatal("the snowball never flew")
	}

	fields := servertest.AwaitBoatMetadata(watcher, cartID)
	if got, ok := fields[int32(protocol.MetadataIndexVehicleShake)]; !ok || got != int32(10) {
		t.Errorf("cart shake ticks = %v (%v), want the ten the snowball's hit laid", got, ok)
	}
	if got := len(srv.MinecartsInDimension("minecraft:overworld")); got != 1 {
		t.Errorf("carts standing = %d, want the one the snowball hit for no damage", got)
	}
	if thrown := srv.ThrownInDimension("minecraft:overworld"); len(thrown) != 0 {
		t.Errorf("%d thrown bodies still in the world, want the snowball broken on the cart", len(thrown))
	}
}
