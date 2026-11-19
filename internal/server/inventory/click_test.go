package inventory

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/registry"
)

// The click logic as ordinary unit tests, driving a Session the way the
// server hands one to each click. The black-box tests in internal/server/*_test.go
// remain the proof of the protocol behaviour around the clicks.

// itemID looks an item up, so the tests name what they mean.
func itemID(t *testing.T, name string) int32 {
	t.Helper()
	id, ok := registry.ItemID(name)
	if !ok {
		t.Fatalf("the item registry has no %s", name)
	}
	return id
}

// slots is a container as the plainest form a test needs.
type slots []protocol.ItemStack

// Slot is what the given one of the slots holds.
func (s slots) Slot(i int) protocol.ItemStack { return s[i] }

// Put replaces what the given one of the slots holds.
func (s slots) Put(i int, stack protocol.ItemStack) { s[i] = stack }

// TestRightClickTakesTheLargerHalf checks the right-click split of a stack:
// the cursor takes the larger half and the slot keeps the floor of them.
func TestRightClickTakesTheLargerHalf(t *testing.T) {
	diamond := protocol.ItemStack{ItemID: itemID(t, "minecraft:diamond"), Count: 7}

	inv := &player.Inventory{}
	inv.Set(player.MainSlot, diamond)
	session := &Session{}
	var outcome Outcome
	session.Click(View{}, inv, protocol.ClickModePickup, 1, player.MainSlot, false, 0, &outcome)

	if got := session.Carried; got.ItemID != diamond.ItemID || got.Count != 4 {
		t.Errorf("carried = %+v, want the larger half of %+v (four of them)", got, diamond)
	}
	if got := inv.Slots[player.MainSlot]; got.ItemID != diamond.ItemID || got.Count != 3 {
		t.Errorf("slot %d = %+v, want the smaller half left in it (three)", player.MainSlot, got)
	}
	if len(outcome.Announce) != 1 || outcome.Announce[0] != player.MainSlot {
		t.Errorf("announced %+v, want just the slot that was split: [%d]",
			outcome.Announce, player.MainSlot)
	}
}

// TestClickOutsideTheWindowDropsTheCursor checks the click on the world
// beyond the window (slot -999): the whole held stack is thrown for a left
// click, one item of it for a right click.
func TestClickOutsideTheWindowDropsTheCursor(t *testing.T) {
	stone := protocol.ItemStack{ItemID: itemID(t, "minecraft:stone"), Count: 32}

	inv := &player.Inventory{}
	session := &Session{Carried: stone}
	var outcome Outcome
	if known := session.Click(View{}, inv, protocol.ClickModePickup, 0, -999, false, 0, &outcome); !known {
		t.Fatal("a click outside the window was not served")
	}
	if len(outcome.Dropped) != 1 || !outcome.Dropped[0].Equal(stone) {
		t.Errorf("dropped %+v, want the whole held stack %+v", outcome.Dropped, stone)
	}
	if !session.Carried.Empty() {
		t.Errorf("carried = %+v after the whole stack was thrown", session.Carried)
	}
	if len(outcome.Announce) != 0 {
		t.Errorf("announced %+v by a click on no slot at all", outcome.Announce)
	}

	session.Carried = stone
	outcome = Outcome{}
	session.Click(View{}, inv, protocol.ClickModePickup, 1, -999, false, 0, &outcome)
	if len(outcome.Dropped) != 1 || !outcome.Dropped[0].Equal(protocol.ItemStack{ItemID: stone.ItemID, Count: 1}) {
		t.Errorf("dropped %+v, want one of the held stack", outcome.Dropped)
	}
	if got := session.Carried; got.ItemID != stone.ItemID || got.Count != 31 {
		t.Errorf("carried = %+v, want the thirty-one the click did not throw", got)
	}
}

// TestThrowFromACraftingResultCraftsAndConsumes checks the Q on a crafting
// result: the grid's worth drops as an item entity and the ingredients are
// spent, where a plain take would have put the result on the cursor.
func TestThrowFromACraftingResultCraftsAndConsumes(t *testing.T) {
	plank := protocol.ItemStack{ItemID: itemID(t, "minecraft:oak_planks"), Count: 1}
	sticks := protocol.ItemStack{ItemID: itemID(t, "minecraft:stick"), Count: 4}

	inv := &player.Inventory{}
	inv.Set(playerCraftingGrid, plank)
	inv.Set(playerCraftingGrid+2, plank)
	session := &Session{}
	var outcome Outcome
	session.Click(View{}, inv, protocol.ClickModeThrow, 0, playerCraftingResult, false, 0, &outcome)

	if len(outcome.Dropped) != 1 || !outcome.Dropped[0].Equal(sticks) {
		t.Errorf("dropped %+v, want %+v", outcome.Dropped, sticks)
	}
	for _, slot := range []int{playerCraftingGrid, playerCraftingGrid + 2} {
		if got := inv.Slots[slot]; !got.Empty() {
			t.Errorf("grid cell %d = %+v after the throw, want the plank consumed", slot, got)
		}
	}
	if !session.Carried.Empty() {
		t.Errorf("carried = %+v, want the cursor untouched", session.Carried)
	}
}

// TestFurnaceOutputGivesButNeverTakes checks a furnace window's output
// slot: with nothing on the cursor it gives what it holds up, whole or by
// the half, marking the take for the furnace's stored experience; with
// the cursor holding the same item, the slot's stack draws onto the
// cursor as far as it has room for. Nothing can be placed into it.
func TestFurnaceOutputGivesButNeverTakes(t *testing.T) {
	ingot := protocol.ItemStack{ItemID: itemID(t, "minecraft:iron_ingot"), Count: 10}
	furnace := slots(make([]protocol.ItemStack, 3))
	furnace[2] = ingot
	view := View{Kind: KindFurnace, Entity: furnace}
	inv := &player.Inventory{}

	session := &Session{}
	var outcome Outcome
	session.Click(view, inv, protocol.ClickModePickup, 0, 2, false, 0, &outcome)
	if got := session.Carried; !got.Equal(ingot) {
		t.Errorf("carried = %+v, want the whole output", got)
	}
	if got := furnace[2]; !got.Empty() {
		t.Errorf("the output slot = %+v, want it empty", got)
	}
	if !outcome.ResultTaken {
		t.Error("the take from the output was not marked as one")
	}

	furnace[2] = ingot
	session = &Session{}
	outcome = Outcome{}
	session.Click(view, inv, protocol.ClickModePickup, 1, 2, false, 0, &outcome)
	if got := session.Carried; got.Count != 5 {
		t.Errorf("carried = %+v, want the larger half (five)", got)
	}
	if got := furnace[2].Count; got != 5 {
		t.Errorf("the output slot holds %d, want the five that stayed", got)
	}
	if !outcome.ResultTaken {
		t.Error("the half take from the output was not marked as one")
	}

	furnace[2] = ingot
	session = &Session{}
	session.Carried = protocol.ItemStack{ItemID: ingot.ItemID, Count: 60}
	outcome = Outcome{}
	session.Click(view, inv, protocol.ClickModePickup, 0, 2, false, 0, &outcome)
	if got := session.Carried.Count; got != 60 {
		t.Errorf("carried = %d, want the cursor left alone, the take being partial", got)
	}
	if got := furnace[2].Count; got != 10 {
		t.Errorf("the output slot holds %d, want all ten left", got)
	}
	if outcome.ResultTaken {
		t.Error("the refused draw was marked as a take")
	}

	furnace[2] = ingot
	session = &Session{}
	session.Carried = protocol.ItemStack{ItemID: ingot.ItemID, Count: 64}
	outcome = Outcome{}
	session.Click(view, inv, protocol.ClickModePickup, 0, 2, false, 0, &outcome)
	if got := furnace[2]; !got.Equal(ingot) {
		t.Errorf("the output slot = %+v, want it untouched", got)
	}
	if outcome.ResultTaken {
		t.Error("a click that took nothing was marked as a take")
	}

	furnace[2] = protocol.ItemStack{}
	session = &Session{}
	session.Carried = ingot
	outcome = Outcome{}
	session.Click(view, inv, protocol.ClickModePickup, 0, 2, false, 0, &outcome)
	if got := furnace[2]; !got.Empty() {
		t.Errorf("the output slot = %+v, want it still empty", got)
	}
	if got := session.Carried; !got.Equal(ingot) {
		t.Errorf("carried = %+v, want the stack kept", got)
	}
}
