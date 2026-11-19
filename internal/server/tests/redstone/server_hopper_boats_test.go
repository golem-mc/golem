package redstone

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

func TestAHopperDrainsAChestBoat(t *testing.T) {
	t.Parallel()
	srv, _ := servertest.OpenWindowed(t, servertest.BlockTestConfig())
	servertest.HoldTicks(t, srv)
	if !srv.PlaceContainer(4, 64, 0, "minecraft:hopper") {
		t.Fatal("no hopper")
	}
	boat := srv.SpawnBoat(4.5, 65, 0.5, "minecraft:oak_chest_boat")
	srv.StowBoatCargo(boat, 0, servertest.ItemStack(t, "minecraft:cobblestone", 5))
	for n := int32(1); n <= 5; n++ {
		servertest.RunTicks(t, srv, 8)
		if got := srv.InventoryAt(4, 64, 0)[0]; got.Count != n || registry.Item(got.ItemID) != registry.ItemCobblestone {
			t.Fatalf("after %d ticks hopper holds %+v, want %d cobblestone", n*8, got, n)
		}
		if got := srv.BoatCargoAt(boat, 0); got.Count != 5-n {
			t.Fatalf("boat holds %+v, want %d left", got, 5-n)
		}
	}
}

func TestADropperFeedsAChestBoat(t *testing.T) {
	t.Parallel()
	srv, _ := servertest.OpenWindowed(t, servertest.BlockTestConfig())
	servertest.HoldTicks(t, srv)
	if !srv.PlaceContainer(4, 64, 0, "minecraft:dropper") {
		t.Fatal("no dropper")
	}
	boat := srv.SpawnBoat(4.5, 64, 1.5, "minecraft:oak_chest_boat")
	dirt := servertest.ItemStack(t, "minecraft:dirt", 1)
	srv.SetInventoryAt(4, 64, 0, 0, dirt)
	srv.Dispense(4, 64, 0, protocol.FaceSouth)
	if got := srv.BoatCargoAt(boat, 0); !got.Equal(dirt) {
		t.Errorf("boat holds %+v, want dirt", got)
	}
	if !servertest.NothingIn(srv.InventoryAt(4, 64, 0)) {
		t.Error("dropper kept the dirt")
	}
	if got := srv.ItemsInDimension(world.DimensionName); len(got) != 0 {
		t.Errorf("dropper spawned items: %+v", got)
	}
}

func TestAHopperFeedsAChestBoat(t *testing.T) {
	t.Parallel()
	srv, _ := servertest.OpenWindowed(t, servertest.BlockTestConfig())
	servertest.HoldTicks(t, srv)
	if !srv.PlaceContainer(4, 64, 0, "minecraft:hopper") {
		t.Fatal("no hopper")
	}
	srv.SetLoadedBlock(4, 64, 0, registry.MustState("minecraft:hopper", map[string]string{"facing": "south"}))
	boat := srv.SpawnBoat(4.5, 64, 1.5, "minecraft:bamboo_chest_raft")
	dirt := servertest.ItemStack(t, "minecraft:dirt", 1)
	srv.SetInventoryAt(4, 64, 0, 0, dirt)
	servertest.RunTicks(t, srv, 8)
	if got := srv.BoatCargoAt(boat, 0); !got.Equal(dirt) {
		t.Errorf("boat holds %+v, want dirt", got)
	}
	if !servertest.NothingIn(srv.InventoryAt(4, 64, 0)) {
		t.Error("hopper kept the dirt")
	}
}
