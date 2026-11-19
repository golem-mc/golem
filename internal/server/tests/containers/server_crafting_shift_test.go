package containers

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
)

// The shift-click on a crafting result: where the craft's output goes is
// what the client predicts as it clicks, and a prediction the server answers
// differently makes the item appear in one slot and jump to another. The
// vanilla menus' quick-move walks the player's things from their far end -
// the hotbar's rightmost slot, then the main rows bottom up - merging into
// the partial stacks it passes first and taking the first empty slot after
// them, and repeats the whole move for as long as the grid keeps being worth
// the same craft. The tests here pin that walk through a real connection,
// slot for slot.

// TestShiftClickCraftResultTakesTheHotbarFromItsEnd crafts one log into four
// planks in the player's own 2x2 and shift-clicks the result into an empty
// inventory: the planks land in the hotbar's rightmost slot, the first place
// the vanilla move tries, not the leftmost one a pickup would take.
func TestShiftClickCraftResultTakesTheHotbarFromItsEnd(t *testing.T) {
	t.Parallel()
	_, w := servertest.OpenWindowed(t, servertest.BlockTestConfig())

	// One log out of the hand and into the grid's first cell, which makes
	// the result worth four planks.
	w.SetCreativeSlot(36, servertest.ItemStack(t, "minecraft:oak_log", 1))
	w.Click(36, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{36: {}},
		servertest.ItemStack(t, "minecraft:oak_log", 1))
	w.Click(1, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{
		1: servertest.ItemStack(t, "minecraft:oak_log", 1),
		0: servertest.ItemStack(t, "minecraft:oak_planks", 4),
	}, protocol.ItemStack{})

	// The shift-click moves the planks to the hotbar's far end and spends
	// the log: the grid is empty, the result nothing, and the correction
	// names the slot the client's own prediction had filled.
	w.Click(0, 0, protocol.ClickModeQuickMove)
	w.ExpectClickResult(map[int16]protocol.ItemStack{
		1:  protocol.ItemStack{},
		0:  protocol.ItemStack{},
		44: servertest.ItemStack(t, "minecraft:oak_planks", 4),
	}, protocol.ItemStack{})
}

// TestShiftClickCraftWithNoRoomSpendsNothing shift-clicks a craft's output
// into an inventory with room for none of it: the vanilla move answers
// nothing and the repeat ends before the craft is spent, so the grid keeps
// its log and the result keeps its planks for a later click.
func TestShiftClickCraftWithNoRoomSpendsNothing(t *testing.T) {
	t.Parallel()
	_, w := servertest.OpenWindowed(t, servertest.BlockTestConfig())

	// The log into the grid out of the hand, and the hand filled again
	// behind it.
	w.SetCreativeSlot(36, servertest.ItemStack(t, "minecraft:oak_log", 1))
	w.Click(36, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{36: {}},
		servertest.ItemStack(t, "minecraft:oak_log", 1))
	w.Click(1, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{
		1: servertest.ItemStack(t, "minecraft:oak_log", 1),
		0: servertest.ItemStack(t, "minecraft:oak_planks", 4),
	}, protocol.ItemStack{})

	// A full inventory, hotbar and main rows alike.
	for slot := int16(9); slot <= 44; slot++ {
		w.SetCreativeSlot(slot, servertest.ItemStack(t, "minecraft:dirt", 64))
	}

	// The shift-click finds no room anywhere and stops: nothing is
	// corrected, the cursor is restated empty, and the log is still in
	// the grid - which the plain click that picks it back out says aloud.
	w.Click(0, 0, protocol.ClickModeQuickMove)
	w.ExpectClickResult(map[int16]protocol.ItemStack{}, protocol.ItemStack{})
	w.Click(1, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{1: {}},
		servertest.ItemStack(t, "minecraft:oak_log", 1))
}
