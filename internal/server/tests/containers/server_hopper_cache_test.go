package containers

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
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
	servertest.TickBlockEntitiesByHand(srv, 8)
	srv.OverworldWorld().BlockEntity(4, 65, 0).Use(func(be *world.BlockEntity) {
		be.Put(0, servertest.ItemStack(t, "minecraft:dirt", 1))
	})
	servertest.TickBlockEntitiesByHand(srv, 1)
	if got := srv.InventoryAt(4, 64, 0)[26]; !got.Equal(dirt) {
		t.Fatalf("chest slot after changing hopper = %+v, want full dirt", got)
	}
}

// TestHopperFullnessAfterSlotEdit checks that a blocked hopper notices its own
// slots changing even when the occupied-slot mask stays the same.
func TestHopperFullnessAfterSlotEdit(t *testing.T) {
	t.Parallel()
	srv := servertest.NewTestServer(t, servertest.BlockTestConfig())
	srv.StopTick()
	t.Cleanup(srv.HoldChunk(4, 0))
	srv.PlaceContainer(4, 66, 0, "minecraft:chest")
	srv.PlaceContainer(4, 65, 0, "minecraft:hopper")
	srv.PlaceContainer(4, 64, 0, "minecraft:chest")
	dirt := servertest.ItemStack(t, "minecraft:dirt", 64)
	for slot := range 27 {
		srv.SetInventoryAt(4, 64, 0, slot, dirt)
	}
	small := servertest.ItemStack(t, "minecraft:stone", 16)
	small.SetMaxStackSize(16)
	fill := func(stack protocol.ItemStack) {
		for slot := range 5 {
			srv.SetInventoryAt(4, 65, 0, slot, stack)
		}
	}
	pulled := func(slot int, want int32) {
		t.Helper()
		servertest.TickBlockEntitiesByHand(srv, 1)
		if got := srv.InventoryAt(4, 65, 0)[slot]; got.Count != want {
			t.Fatalf("hopper slot %d count = %d, want %d", slot, got.Count, want)
		}
	}
	srv.SetInventoryAt(4, 66, 0, 0, servertest.ItemStack(t, "minecraft:stone", 64))

	// A full hopper pulls nothing, then pulls once a full stack shrinks.
	full := servertest.ItemStack(t, "minecraft:stone", 64)
	fill(full)
	pulled(0, 64)
	shrunk := full
	shrunk.Count = 63
	srv.SetInventoryAt(4, 65, 0, 0, shrunk)
	pulled(0, 64)

	// Raising max_stack_size on a full stack makes room at unchanged occupancy.
	bigger := servertest.ItemStack(t, "minecraft:stone", 16)
	bigger.SetMaxStackSize(64)
	source := servertest.ItemStack(t, "minecraft:stone", 64)
	source.SetMaxStackSize(64)
	srv.SetInventoryAt(4, 66, 0, 0, source)
	fill(small)
	// Outlast the cooldown of the pull above.
	servertest.TickBlockEntitiesByHand(srv, 7)
	pulled(0, 16)
	srv.SetInventoryAt(4, 65, 0, 0, bigger)
	pulled(0, 17)
}
