package containers

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
)

// What a closing window hands back. Closing the player's own window returns
// the cursor's stack and then the 2x2 grid to the inventory - vanilla's
// InventoryMenu.removed, by Inventory.placeItemBackInInventory: onto a
// partial stack in the held slot, the off hand, then the hotbar and main
// rows, and only then into an empty hotbar or main slot, the rest dropped. A
// player who leaves the world - disconnected, or the dead body the respawn
// removes - has it all dropped where they are.

func TestClosingTheInventoryPutsTheGridAndCursorBack(t *testing.T) {
	t.Parallel()
	srv, w := servertest.OpenWindowed(t, servertest.BlockTestConfig())

	// The held slot and the off hand are topped up before the hotbar slot
	// that comes first in the inventory.
	srv.GiveSlot("Testificate", 36, servertest.ItemStack(t, "minecraft:oak_log", 60))
	srv.GiveSlot("Testificate", 37, servertest.ItemStack(t, "minecraft:oak_log", 10))
	srv.GiveSlot("Testificate", 45, servertest.ItemStack(t, "minecraft:oak_log", 10))
	srv.GiveSlot("Testificate", 38, servertest.ItemStack(t, "minecraft:oak_log", 64))

	// One log in each of two cells, sixty-two left on the cursor.
	w.PutInGrid(38, 1, 2)

	w.CloseWindow()
	slots, carried := w.AwaitWindowZero()
	want := map[int]protocol.ItemStack{
		0:  {},
		1:  {},
		2:  {},
		36: servertest.ItemStack(t, "minecraft:oak_log", 64),
		45: servertest.ItemStack(t, "minecraft:oak_log", 64),
		37: servertest.ItemStack(t, "minecraft:oak_log", 16),
		38: {},
	}
	for slot, stack := range want {
		if got := slots[slot]; !got.Equal(stack) {
			t.Errorf("after close, slot %d = %+v, want %+v", slot, got, stack)
		}
		if got := srv.InventorySlot("Testificate", slot); slot != 0 && !got.Equal(stack) {
			t.Errorf("server's slot %d = %+v, want %+v", slot, got, stack)
		}
	}
	if !carried.Empty() {
		t.Errorf("cursor after close = %+v, want nothing", carried)
	}
}
