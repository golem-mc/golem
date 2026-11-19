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

// TestShiftClickCraftFillsTheMainRowsPastAFullHotbar shift-clicks a craft's
// output into an inventory whose hotbar is full: the walk goes on past it
// into the main inventory, where the first empty slot from the far end is
// the bottom row's rightmost one.
func TestShiftClickCraftFillsTheMainRowsPastAFullHotbar(t *testing.T) {
	t.Parallel()
	_, w := servertest.OpenWindowed(t, servertest.BlockTestConfig())

	// The log goes into the grid while there is still a hand to take it
	// from, and the hand's slot is filled again behind it.
	w.SetCreativeSlot(36, servertest.ItemStack(t, "minecraft:oak_log", 1))
	w.Click(36, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{36: {}},
		servertest.ItemStack(t, "minecraft:oak_log", 1))
	w.Click(1, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{
		1: servertest.ItemStack(t, "minecraft:oak_log", 1),
		0: servertest.ItemStack(t, "minecraft:oak_planks", 4),
	}, protocol.ItemStack{})

	// A full hotbar: nine slots, every one a stack of dirt. The main
	// rows behind it stay empty for the walk to fall into.
	for slot := int16(36); slot <= 44; slot++ {
		w.SetCreativeSlot(slot, servertest.ItemStack(t, "minecraft:dirt", 64))
	}

	// The shift-click walks the hotbar from its end, finds it full, and
	// lands the planks in the main inventory's bottom row, at its right.
	w.Click(0, 0, protocol.ClickModeQuickMove)
	w.ExpectClickResult(map[int16]protocol.ItemStack{
		1:  protocol.ItemStack{},
		0:  protocol.ItemStack{},
		35: servertest.ItemStack(t, "minecraft:oak_planks", 4),
	}, protocol.ItemStack{})
}

// TestShiftClickCraftMergesIntoThePartialStackTheFarEndFinds shift-clicks a
// craft's output onto an inventory holding two part-stacks of the same item:
// the merge pass walks from the far end too, so the stack nearer the hotbar's
// right edge takes the craft and the one near its left keeps what it had.
func TestShiftClickCraftMergesIntoThePartialStackTheFarEndFinds(t *testing.T) {
	t.Parallel()
	_, w := servertest.OpenWindowed(t, servertest.BlockTestConfig())

	// Two part-stacks of planks: sixty-two in the hotbar's third slot and
	// thirty in its seventh, and the log into the grid out of the hand.
	w.SetCreativeSlot(38, servertest.ItemStack(t, "minecraft:oak_planks", 62))
	w.SetCreativeSlot(42, servertest.ItemStack(t, "minecraft:oak_planks", 30))
	w.SetCreativeSlot(36, servertest.ItemStack(t, "minecraft:oak_log", 1))
	w.Click(36, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{36: {}},
		servertest.ItemStack(t, "minecraft:oak_log", 1))
	w.Click(1, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{
		1: servertest.ItemStack(t, "minecraft:oak_log", 1),
		0: servertest.ItemStack(t, "minecraft:oak_planks", 4),
	}, protocol.ItemStack{})

	// The four planks the craft makes join the thirty at slot 42, the
	// first part-stack the far-end walk comes to: slot 38 is not part of
	// the answer at all.
	w.Click(0, 0, protocol.ClickModeQuickMove)
	w.ExpectClickResult(map[int16]protocol.ItemStack{
		1:  protocol.ItemStack{},
		0:  protocol.ItemStack{},
		42: servertest.ItemStack(t, "minecraft:oak_planks", 34),
	}, protocol.ItemStack{})

	// And the stack at 38 still holds the sixty-two it started with,
	// which the plain click that picks it up says aloud.
	w.Click(38, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{},
		servertest.ItemStack(t, "minecraft:oak_planks", 62))
}

// TestShiftClickCraftsTheTableDryFromTheHotbarsFarEnd fills a crafting
// table's grid with half-stacks of planks and shift-clicks the sticks they
// are worth: thirty-two crafts come out of the two cells, a craft a plank
// out of each, piling onto the hotbar's rightmost slot until it is full and
// then the one beside it, the way the vanilla click repeats its move while
// the grid keeps answering with the same craft.
func TestShiftClickCraftsTheTableDryFromTheHotbarsFarEnd(t *testing.T) {
	t.Parallel()
	_, w := servertest.OpenWindowed(t, servertest.BlockTestConfig())

	// A table on the ground, opened with a right-click.
	w.SetCreativeSlot(36, servertest.ItemStack(t, "minecraft:crafting_table", 1))
	w.PlaceBlock(4, 63, 0, protocol.FaceTop)
	w.ExpectBlockAck()
	w.UseBlock(4, 64, 0)
	w.ExpectContainerOpen(protocol.MenuCrafting, false, 0, 0, 0)

	// A spare table item parked in the offhand, out of the walk's way,
	// and two stacks of planks in the hotbar's first two slots - window
	// 37 and 38 in the table's numbering - each split in half with a
	// right-click pickup, one half laid into each of the left column's
	// top two cells, which is the sticks' pattern whatever the cells
	// count.
	w.SetCreativeSlot(45, servertest.ItemStack(t, "minecraft:crafting_table", 1))
	w.SetCreativeSlot(36, servertest.ItemStack(t, "minecraft:oak_planks", 64))
	w.SetCreativeSlot(37, servertest.ItemStack(t, "minecraft:oak_planks", 64))
	w.Click(37, 1, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{
		37: servertest.ItemStack(t, "minecraft:oak_planks", 32),
	}, servertest.ItemStack(t, "minecraft:oak_planks", 32))
	w.Click(1, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{
		1: servertest.ItemStack(t, "minecraft:oak_planks", 32),
		// One cell of planks alone is worth what one plank is: a button,
		// the column of two being what turns it into sticks.
		0: servertest.ItemStack(t, "minecraft:oak_button", 1),
	}, protocol.ItemStack{})
	w.Click(38, 1, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{
		38: servertest.ItemStack(t, "minecraft:oak_planks", 32),
	}, servertest.ItemStack(t, "minecraft:oak_planks", 32))
	w.Click(4, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{
		4: servertest.ItemStack(t, "minecraft:oak_planks", 32),
		0: servertest.ItemStack(t, "minecraft:stick", 4),
	}, protocol.ItemStack{})

	// Thirty-two crafts, four sticks each: the first sixteen fill the
	// hotbar's rightmost slot and the rest the one beside it, each craft
	// taking one plank out of both cells on its way, until the grid is
	// spent and the result worth nothing.
	w.Click(0, 0, protocol.ClickModeQuickMove)
	w.ExpectClickResult(map[int16]protocol.ItemStack{
		1:  protocol.ItemStack{},
		4:  protocol.ItemStack{},
		0:  protocol.ItemStack{},
		45: servertest.ItemStack(t, "minecraft:stick", 64),
		44: servertest.ItemStack(t, "minecraft:stick", 64),
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
