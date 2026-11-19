package containers

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/player"
)

// A number key or F on the crafting result crafts into the empty slot the
// key names, the take spending the grid and handing back the remainders; a
// slot that is occupied leaves the click doing nothing.
// vanilla: AbstractContainerMenu.doClick SWAP

// TestANumberKeyOnTheCraftingResultCraftsIntoAnEmptyHotbarSlot puts a log in
// the player's 2x2 and presses 1 on the result: the planks land in the first
// hotbar slot, the log is spent, and the cursor stays empty.
func TestANumberKeyOnTheCraftingResultCraftsIntoAnEmptyHotbarSlot(t *testing.T) {
	t.Parallel()
	srv, w := servertest.OpenWindowed(t, servertest.BlockTestConfig())

	srv.GiveSlot("Testificate", 9, servertest.ItemStack(t, "minecraft:oak_log", 1))
	w.Drain()
	w.PutInGrid(9, 1)

	w.Click(0, 0, protocol.ClickModeSwap)
	w.ExpectClickResult(map[int16]protocol.ItemStack{
		0:  {},
		1:  {},
		36: servertest.ItemStack(t, "minecraft:oak_planks", 4),
	}, protocol.ItemStack{})
	if got := srv.InventorySlot("Testificate", player.HotbarSlot); !got.Equal(servertest.ItemStack(t, "minecraft:oak_planks", 4)) {
		t.Errorf("hotbar slot 1 = %+v, want four planks", got)
	}
}

// TestFOnTheCraftingResultCraftsIntoTheOffHand is the same with the off-hand
// swap key.
func TestFOnTheCraftingResultCraftsIntoTheOffHand(t *testing.T) {
	t.Parallel()
	srv, w := servertest.OpenWindowed(t, servertest.BlockTestConfig())

	srv.GiveSlot("Testificate", 9, servertest.ItemStack(t, "minecraft:oak_log", 1))
	w.Drain()
	w.PutInGrid(9, 1)

	w.Click(0, 40, protocol.ClickModeSwap)
	w.ExpectClickResult(map[int16]protocol.ItemStack{
		0:  {},
		1:  {},
		45: servertest.ItemStack(t, "minecraft:oak_planks", 4),
	}, protocol.ItemStack{})
}

// TestANumberKeyOnTheCraftingResultNeedsAnEmptySlot leaves the grid alone
// when the hotbar slot already holds something.
func TestANumberKeyOnTheCraftingResultNeedsAnEmptySlot(t *testing.T) {
	t.Parallel()
	srv, w := servertest.OpenWindowed(t, servertest.BlockTestConfig())

	srv.GiveSlot("Testificate", 9, servertest.ItemStack(t, "minecraft:oak_log", 1))
	srv.GiveSlot("Testificate", player.HotbarSlot, servertest.ItemStack(t, "minecraft:dirt", 1))
	w.Drain()
	w.PutInGrid(9, 1)

	w.Click(0, 0, protocol.ClickModeSwap)
	w.ExpectClickResult(map[int16]protocol.ItemStack{}, protocol.ItemStack{})
	if got := srv.InventorySlot("Testificate", 1); !got.Equal(servertest.ItemStack(t, "minecraft:oak_log", 1)) {
		t.Errorf("grid cell 1 = %+v, want the log kept", got)
	}
	if got := srv.InventorySlot("Testificate", player.HotbarSlot); !got.Equal(servertest.ItemStack(t, "minecraft:dirt", 1)) {
		t.Errorf("hotbar slot 1 = %+v, want the dirt untouched", got)
	}
}

// swapOnATableResult sets a crafting table's grid to one log and presses
// button on the result.
func swapOnATableResult(t *testing.T, button int8) (*server.Server, *servertest.WindowedClient) {
	t.Helper()
	srv, w := servertest.OpenWindowed(t, servertest.BlockTestConfig())

	w.SetCreativeSlot(36, servertest.ItemStack(t, "minecraft:crafting_table", 1))
	w.PlaceBlock(4, 63, 0, protocol.FaceTop)
	w.ExpectBlockAck()
	w.UseBlock(4, 64, 0)
	w.ExpectContainerOpen(protocol.MenuCrafting, false, 0, 0, 0)

	srv.GiveSlot("Testificate", player.HotbarSlot, servertest.ItemStack(t, "minecraft:oak_log", 1))
	w.Click(37, 0, protocol.ClickModePickup)
	w.SettleTrackingState()
	w.Click(1, 1, protocol.ClickModePickup)
	w.SettleTrackingState()

	w.Click(0, button, protocol.ClickModeSwap)
	return srv, w
}
