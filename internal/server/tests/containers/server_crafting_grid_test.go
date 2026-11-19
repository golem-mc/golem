package containers

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
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

func TestClosingAFullInventoryDropsTheGridAndCursor(t *testing.T) {
	t.Parallel()
	srv, w := servertest.OpenWindowed(t, servertest.BlockTestConfig())

	dirt := servertest.ItemStack(t, "minecraft:dirt", 64)
	for slot := player.MainSlot; slot < player.HotbarSlot+player.HotbarSlots; slot++ {
		srv.GiveSlot("Testificate", slot, dirt)
	}
	stone := servertest.ItemStack(t, "minecraft:stone", 64)
	srv.GiveSlot("Testificate", 1, stone)

	// A whole stack of dirt on the cursor, its slot filled again behind it.
	w.Click(9, 0, protocol.ClickModePickup)
	w.SettleTrackingState()
	srv.GiveSlot("Testificate", 9, dirt)

	w.CloseWindow()
	slots, carried := w.AwaitWindowZero()
	if !slots[1].Empty() || !carried.Empty() {
		t.Errorf("after close, grid cell = %+v and cursor = %+v, want both empty", slots[1], carried)
	}
	// An empty off hand is only ever merged into, never filled.
	if !slots[player.OffHandSlot].Empty() {
		t.Errorf("off hand = %+v, want empty", slots[player.OffHandSlot])
	}
	// The cursor's stack goes first.
	w.AwaitDrops(2)
	w.ExpectDrop(dirt)
	w.ExpectDrop(stone)
}

func TestLeavingDropsTheGridAndCursor(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.BlockTestConfig())
	leaver := servertest.WindowedOf(t, "Leaver", connect())
	watcher := servertest.WindowedOf(t, "Watcher", connect())

	srv.GiveSlot("Leaver", 36, servertest.ItemStack(t, "minecraft:oak_log", 5))
	leaver.PutInGrid(36, 1)
	leaver.Disconnect()

	// Both stacks lie at the player's feet: the cursor's first, then the
	// grid's.
	watcher.AwaitDrops(2)
	watcher.ExpectDrop(servertest.ItemStack(t, "minecraft:oak_log", 4))
	watcher.ExpectDrop(servertest.ItemStack(t, "minecraft:oak_log", 1))

	// And they are not in the inventory saved with the player as well.
	servertest.WindowedOf(t, "Leaver", connect())
	for _, slot := range []int{1, 36} {
		if got := srv.InventorySlot("Leaver", slot); !got.Empty() {
			t.Errorf("rejoined with slot %d = %+v, want empty", slot, got)
		}
	}
}

func TestTheGridIsDroppedWhenTheDeadBodyGoes(t *testing.T) {
	t.Parallel()
	cfg := servertest.SurvivalConfig()
	srv, connect := servertest.Start(t, cfg)
	dier := servertest.WindowedOf(t, "Dier", connect())
	watcher := servertest.WindowedOf(t, "Watcher", connect())

	logs := servertest.ItemStack(t, "minecraft:oak_log", 3)
	srv.GiveSlot("Dier", 1, logs)

	// The death drops the inventory, which the grid is not part of.
	srv.Hurt("Dier", 100)
	if n := srv.ItemCount(); n != 0 {
		t.Fatalf("%d items dropped by the death, want none", n)
	}

	// The respawn removes the body, and the grid's stack falls where it lay.
	dier.Send(protocol.IDPlayClientCommandIn, func(b *protocol.Buffer) {
		b.PutVarInt(protocol.ClientCommandPerformRespawn)
	})
	watcher.AwaitDrops(1)
	watcher.ExpectDrop(logs)
	if got := srv.InventorySlot("Dier", 1); !got.Empty() {
		t.Errorf("grid cell after respawn = %+v, want empty", got)
	}
}

// A crafting table hands back the cursor before its grid, so with one slot
// free the cursor's stack takes it and the grid's spills. vanilla:
// CraftingMenu.removed calls super.removed before clearContainer.
func TestACraftingTableHandsBackTheCursorBeforeTheGrid(t *testing.T) {
	t.Parallel()
	srv, w := servertest.OpenWindowed(t, servertest.BlockTestConfig())

	w.SetCreativeSlot(36, servertest.ItemStack(t, "minecraft:crafting_table", 1))
	w.PlaceBlock(4, 63, 0, protocol.FaceTop)
	w.ExpectBlockAck()
	w.UseBlock(4, 64, 0)
	w.ExpectContainerOpen(protocol.MenuCrafting, false, 0, 0, 0)

	dirt := servertest.ItemStack(t, "minecraft:dirt", 64)
	for slot := player.MainSlot; slot < player.HotbarSlot+player.HotbarSlots; slot++ {
		srv.GiveSlot("Testificate", slot, dirt)
	}
	stone := servertest.ItemStack(t, "minecraft:stone", 64)
	log := servertest.ItemStack(t, "minecraft:oak_log", 1)
	srv.GiveSlot("Testificate", 36, stone)
	srv.GiveSlot("Testificate", 37, log)

	// The table's window numbers the hotbar from 37. The stone goes into
	// the grid and the log onto the cursor, leaving the stone's slot the
	// only free one.
	w.Click(37, 0, protocol.ClickModePickup)
	w.SettleTrackingState()
	w.Click(1, 0, protocol.ClickModePickup)
	w.SettleTrackingState()
	w.Click(38, 0, protocol.ClickModePickup)
	w.SettleTrackingState()
	srv.GiveSlot("Testificate", 37, dirt)

	w.CloseWindow()
	slots, carried := w.AwaitWindowZero()
	if got := slots[36]; !got.Equal(log) {
		t.Errorf("free slot = %+v, want the cursor's %+v", got, log)
	}
	if !carried.Empty() {
		t.Errorf("cursor after close = %+v, want nothing", carried)
	}
	w.AwaitDrops(1)
	w.ExpectDrop(stone)
}
