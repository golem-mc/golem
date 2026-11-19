package items

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// TestAStacksOwnMaxStackSizeSetsItsSlotLimit: /give splits a stack by its own
// max_stack_size, a shift-click into a chest keeps that size, the saved chest
// reloads it, and a damageable item cannot be made stackable.
// vanilla: ItemStack.getMaxStackSize, ItemStack.validateComponents
func TestAStacksOwnMaxStackSizeSetsItsSlotLimit(t *testing.T) {
	t.Parallel()
	srv, w := servertest.OpenWindowedOps(t, servertest.BlockTestConfig())

	w.ChatCommand("give @s diamond_sword[max_stack_size=64]")
	w.ChatCommand("give @s stick[max_stack_size=16] 40")
	servertest.UntilTicks(t, srv, 20, "the third stack", func() bool {
		return !srv.InventorySlot("Testificate", player.HotbarSlot+2).Empty()
	})
	for slot, want := range []int32{16, 16, 8} {
		got := srv.InventorySlot("Testificate", player.HotbarSlot+slot)
		if got.ItemID != servertest.MustItem("minecraft:stick") || got.Count != want {
			t.Fatalf("hotbar slot %d = %d of item %d, want %d sticks", slot, got.Count, got.ItemID, want)
		}
	}
	if got := srv.InventorySlot("Testificate", player.HotbarSlot+3); !got.Empty() {
		t.Fatalf("a fourth slot holds %+v; the sword must be refused", got)
	}

	if !srv.SetBlock(4, 64, 0, servertest.BlockState(t, "minecraft:chest")) {
		t.Fatal("the chest was not placed")
	}
	w.Settle()
	w.UseBlock(4, 64, 0)
	w.ExpectContainerOpen(protocol.MenuGeneric9x3, true, 4, 64, 0)
	for slot := range int16(3) {
		w.Click(27+27+slot, 0, protocol.ClickModeQuickMove)
		w.Settle()
	}
	chest := srv.InventoryAt(4, 64, 0)
	for slot, want := range []int32{16, 16, 8} {
		if chest[slot].Count != want {
			t.Errorf("chest slot %d holds %d, want %d", slot, chest[slot].Count, want)
		}
	}

	w.ChatCommand(`data get block 4 64 0 Items[0].components."minecraft:max_stack_size"`)
	w.ExpectCommandFeedback("commands.data.block.query", "4", "64", "0", "16")
}

// TestAPickedUpStackMergesUpToItsOwnMaxStackSize: an item entity of
// stone[max_stack_size=4] tops a partial slot up to 4 and the rest takes a new
// slot.
// vanilla: ItemStack.getMaxStackSize, Inventory.add
func TestAPickedUpStackMergesUpToItsOwnMaxStackSize(t *testing.T) {
	t.Parallel()
	srv, w := servertest.OpenWindowedOps(t, servertest.BlockTestConfig())

	w.ChatCommand("give @s stone[max_stack_size=4] 6")
	servertest.UntilTicks(t, srv, 20, "the second stack", func() bool {
		return !srv.InventorySlot("Testificate", player.HotbarSlot+1).Empty()
	})
	w.ChatCommand(`summon item ~ ~ ~ {Item:{id:"minecraft:stone",count:4,components:{"minecraft:max_stack_size":4}},PickupDelay:0}`)
	servertest.UntilTicks(t, srv, 60, "the third stack", func() bool {
		return !srv.InventorySlot("Testificate", player.HotbarSlot+2).Empty()
	})
	for slot, want := range []int32{4, 4, 2} {
		got := srv.InventorySlot("Testificate", player.HotbarSlot+slot)
		if got.ItemID != servertest.MustItem("minecraft:stone") || got.Count != want {
			t.Fatalf("hotbar slot %d = %d of item %d, want %d stone", slot, got.Count, got.ItemID, want)
		}
	}
}

// TestABundleTakesAWholeStackOfItsOwnMaxSize: ender pearls with
// max_stack_size=64 weigh 1/64 each, so a bundle takes 64 where plain pearls
// (16) would stop at 16.
// vanilla: BundleContents.getWeight
func TestABundleTakesAWholeStackOfItsOwnMaxSize(t *testing.T) {
	t.Parallel()
	_, w := servertest.OpenWindowed(t, servertest.BlockTestConfig())

	pearls := servertest.ItemStack(t, "minecraft:ender_pearl", 64)
	pearls.SetMaxStackSize(64)
	bundle := protocol.ItemStack{ItemID: servertest.ItemID(t, "minecraft:bundle"), Count: 1}
	w.SetCreativeSlot(36, pearls)
	w.SetCreativeSlot(37, bundle)

	w.Click(36, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{36: {}}, pearls)
	w.Click(37, 0, protocol.ClickModePickup)
	var contents protocol.Buffer
	contents.PutVarInt(1) // one entry: the 64 pearls with their max_stack_size patch
	contents.PutVarInt(pearls.ItemID)
	contents.PutVarInt(64)
	contents.PutVarInt(1)
	contents.PutVarInt(0)
	contents.PutVarInt(protocol.DataComponentMaxStackSize)
	contents.PutVarInt(64)
	bundle.Raw = []protocol.RawComponent{{Type: protocol.DataComponentBundleContents, Data: contents.Bytes()}}
	w.ExpectClickResult(map[int16]protocol.ItemStack{37: bundle}, protocol.ItemStack{})
}
