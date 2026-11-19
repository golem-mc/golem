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
