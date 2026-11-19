package containers

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

func TestAHopperRetriesAfterEitherInventoryChanges(t *testing.T) {
	t.Parallel()
	srv, _ := servertest.Join(t, servertest.BlockTestConfig(), "Tester")
	servertest.HoldTicks(t, srv)
	srv.PlaceContainer(4, 65, 0, "minecraft:hopper")
	srv.PlaceContainer(4, 64, 0, "minecraft:chest")
	dirt := servertest.ItemStack(t, "minecraft:dirt", 64)
	stone := servertest.ItemStack(t, "minecraft:stone", 64)
	for slot := range 27 {
		srv.SetInventoryAt(4, 64, 0, slot, dirt)
	}
	srv.SetInventoryAt(4, 65, 0, 0, stone)
	servertest.TickBlockEntitiesByHand(srv, 3)
	if got := srv.HopperCooldownAt(4, 65, 0); got != 0 {
		t.Fatalf("blocked hopper cooldown = %d, want 0", got)
	}
	// A vacancy wakes the failed push immediately, without an idle cooldown.
	srv.OverworldWorld().BlockEntity(4, 64, 0).Use(func(be *world.BlockEntity) {
		be.Put(26, protocol.ItemStack{})
	})
	servertest.TickBlockEntitiesByHand(srv, 1)
	if got := srv.InventoryAt(4, 64, 0)[26]; got.ItemID != stone.ItemID || got.Count != 1 {
		t.Fatalf("opened chest slot = %+v, want one stone", got)
	}
	// With only room for dirt, changing the hopper's item must retry too.
	srv.SetInventoryAt(4, 64, 0, 26, servertest.ItemStack(t, "minecraft:dirt", 63))
	servertest.TickBlockEntitiesByHand(srv, 10)
	srv.OverworldWorld().BlockEntity(4, 65, 0).Use(func(be *world.BlockEntity) {
		be.Put(0, servertest.ItemStack(t, "minecraft:dirt", 1))
	})
	servertest.TickBlockEntitiesByHand(srv, 1)
	if got := srv.InventoryAt(4, 64, 0)[26]; !got.Equal(dirt) {
		t.Fatalf("chest slot after changing hopper = %+v, want full dirt", got)
	}
}

func TestAHopperRefreshesMissingAndReplacedSources(t *testing.T) {
	t.Parallel()
	srv, _ := servertest.Join(t, servertest.BlockTestConfig(), "Tester")
	servertest.HoldTicks(t, srv)
	srv.PlaceContainer(4, 64, 0, "minecraft:hopper")
	servertest.TickBlockEntitiesByHand(srv, 3)
	srv.PlaceContainer(4, 65, 0, "minecraft:chest")
	servertest.TickBlockEntitiesByHand(srv, 3)
	srv.SetInventoryAt(4, 65, 0, 0, servertest.ItemStack(t, "minecraft:stone", 1))
	servertest.TickBlockEntitiesByHand(srv, 1)
	if got := srv.InventoryAt(4, 64, 0)[0]; got.Count != 1 {
		t.Fatalf("hopper after filling source = %+v, want one stone", got)
	}
	srv.RemoveBlockEntity(4, 65, 0)
	srv.PlaceContainer(4, 65, 0, "minecraft:barrel")
	srv.SetInventoryAt(4, 65, 0, 0, servertest.ItemStack(t, "minecraft:dirt", 1))
	servertest.TickBlockEntitiesByHand(srv, 8)
	if got := srv.InventoryAt(4, 64, 0)[1]; got.ItemID != int32(registry.ItemDirt) || got.Count != 1 {
		t.Fatalf("hopper after replacing source = %+v, want one dirt", got)
	}
}

func TestAHopperRefreshesAChestPartnerAcrossAChunkBoundary(t *testing.T) {
	t.Parallel()
	srv, _ := servertest.Join(t, servertest.BlockTestConfig(), "Tester")
	servertest.HoldTicks(t, srv)
	release := srv.HoldChunk(16, 0)
	defer release()
	w := srv.OverworldWorld()
	place := func(x int32, typ string) {
		w.SetBlock(x, 65, 0, registry.MustState("minecraft:chest", map[string]string{"facing": "north", "type": typ}))
		w.EnsureBlockEntity(x, 65, 0, world.KindChest)
	}
	srv.PlaceContainer(15, 64, 0, "minecraft:hopper")
	place(15, "left")
	place(16, "right")
	servertest.TickBlockEntitiesByHand(srv, 3)
	// Replace only the remote half; the local half's block and inventory stay.
	srv.RemoveBlockEntity(16, 65, 0)
	place(16, "right")
	srv.SetInventoryAt(16, 65, 0, 0, servertest.ItemStack(t, "minecraft:dirt", 1))
	servertest.TickBlockEntitiesByHand(srv, 1)
	if got := srv.InventoryAt(15, 64, 0)[0]; got.ItemID != int32(registry.ItemDirt) || got.Count != 1 {
		t.Fatalf("hopper after replacing remote half = %+v, want one dirt", got)
	}
}

func TestAHopperRetriesWhenVehicleCargoChanges(t *testing.T) {
	for _, kind := range []string{"cart", "boat"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			srv, _ := servertest.Join(t, servertest.BlockTestConfig(), "Tester")
			servertest.HoldTicks(t, srv)
			srv.PlaceContainer(4, 64, 0, "minecraft:hopper")
			stow := srv.StowMinecartCargo
			var id int32
			if kind == "cart" {
				id = srv.SpawnMinecartOfKind("minecraft:chest_minecart", 4.5, 65.0625, 0.5)
			} else {
				id = srv.SpawnBoat(4.5, 65, 0.5, "minecraft:oak_chest_boat")
				stow = srv.StowBoatCargo
			}
			servertest.TickBlockEntitiesByHand(srv, 3)
			stow(id, 0, servertest.ItemStack(t, "minecraft:dirt", 1))
			servertest.TickBlockEntitiesByHand(srv, 1)
			if got := srv.InventoryAt(4, 64, 0)[0]; got.ItemID != int32(registry.ItemDirt) || got.Count != 1 {
				t.Fatalf("hopper after filling %s = %+v, want one dirt", kind, got)
			}
		})
	}
}

func TestAHopperRefreshesItsSpoutAfterFacingChanges(t *testing.T) {
	t.Parallel()
	srv, _ := servertest.Join(t, servertest.BlockTestConfig(), "Tester")
	servertest.HoldTicks(t, srv)
	srv.PlaceContainer(4, 65, 0, "minecraft:hopper")
	srv.PlaceContainer(5, 65, 0, "minecraft:barrel")
	srv.SetInventoryAt(4, 65, 0, 0, servertest.ItemStack(t, "minecraft:dirt", 1))
	servertest.TickBlockEntitiesByHand(srv, 3)
	srv.SetLoadedBlock(4, 65, 0, registry.MustState("minecraft:hopper", map[string]string{"facing": "east", "enabled": "true"}))
	servertest.TickBlockEntitiesByHand(srv, 1)
	if got := srv.InventoryAt(5, 65, 0)[0]; got.ItemID != int32(registry.ItemDirt) || got.Count != 1 {
		t.Fatalf("new spout target = %+v, want one dirt", got)
	}
}

func TestAHopperRetriesWhenAComposterBecomesReady(t *testing.T) {
	t.Parallel()
	srv, _ := servertest.Join(t, servertest.BlockTestConfig(), "Tester")
	servertest.HoldTicks(t, srv)
	srv.PlaceContainer(4, 64, 0, "minecraft:hopper")
	srv.SetLoadedBlock(4, 65, 0, registry.MustState("minecraft:composter", map[string]string{"level": "7"}))
	servertest.TickBlockEntitiesByHand(srv, 3)
	srv.SetLoadedBlock(4, 65, 0, registry.MustState("minecraft:composter", map[string]string{"level": "8"}))
	servertest.TickBlockEntitiesByHand(srv, 1)
	if got := srv.InventoryAt(4, 64, 0)[0]; got.ItemID != int32(registry.ItemBoneMeal) || got.Count != 1 {
		t.Fatalf("hopper after composter ripens = %+v, want one bone meal", got)
	}
}

func TestAHopperRetriesWhenACrafterSlotIsEnabled(t *testing.T) {
	t.Parallel()
	srv, _ := servertest.Join(t, servertest.BlockTestConfig(), "Tester")
	servertest.HoldTicks(t, srv)
	srv.PlaceContainer(4, 65, 0, "minecraft:hopper")
	srv.PlaceContainer(4, 64, 0, "minecraft:crafter")
	crafter := srv.OverworldWorld().BlockEntity(4, 64, 0)
	crafter.Use(func(be *world.BlockEntity) {
		for slot := range 9 {
			be.SetCrafterSlotState(slot, false)
		}
	})
	srv.SetInventoryAt(4, 65, 0, 0, servertest.ItemStack(t, "minecraft:dirt", 1))
	servertest.TickBlockEntitiesByHand(srv, 3)
	crafter.Use(func(be *world.BlockEntity) { be.SetCrafterSlotState(8, true) })
	servertest.TickBlockEntitiesByHand(srv, 1)
	if got := srv.InventoryAt(4, 64, 0)[8]; got.ItemID != int32(registry.ItemDirt) || got.Count != 1 {
		t.Fatalf("enabled crafter slot = %+v, want one dirt", got)
	}
}
