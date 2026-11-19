package inventory

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
)

// The double-click gather (mode 6) as ordinary unit tests, driving a
// Session over a chest the way the server hands one to each click.

// TestCollectFillsFromPartialStacksFirst checks the sweep's two passes: a
// full stack sitting ahead of a partial one is only opened for the
// remainder, since the first pass skips the stacks already full.
func TestCollectFillsFromPartialStacksFirst(t *testing.T) {
	stone := itemID(t, "minecraft:stone")
	full := protocol.ItemStack{ItemID: stone, Count: 64}
	partial := protocol.ItemStack{ItemID: stone, Count: 30}

	chest := make(slots, 27)
	chest[0] = full
	chest[1] = partial
	view := View{Kind: KindChest, Entity: chest}
	inv := &player.Inventory{}
	session := &Session{Carried: protocol.ItemStack{ItemID: stone, Count: 10}}
	var outcome Outcome

	session.Click(view, inv, protocol.ClickModeCollect, 0, 5, false, 0, &outcome)

	if got := chest[0]; got.Count != 40 {
		t.Errorf("chest slot 0 = %+v, want the forty the full stack was left with", got)
	}
	if got := chest[1]; !got.Empty() {
		t.Errorf("chest slot 1 = %+v, want it drained into the cursor", got)
	}
	if got := session.Carried.Count; got != 64 {
		t.Errorf("carried = %d, want the whole stack the gather filled", got)
	}
}

// TestCollectOnAStackStillHoldingTheItemDoesNothing checks the clicked
// slot's own condition: the gather runs only when that slot is empty, so a
// double click on a slot that still holds the item leaves the window alone
// even though another slot holds the same item.
func TestCollectOnAStackStillHoldingTheItemDoesNothing(t *testing.T) {
	stone := itemID(t, "minecraft:stone")

	chest := make(slots, 27)
	chest[0] = protocol.ItemStack{ItemID: stone, Count: 20}
	chest[1] = protocol.ItemStack{ItemID: stone, Count: 30}
	view := View{Kind: KindChest, Entity: chest}
	inv := &player.Inventory{}
	session := &Session{Carried: protocol.ItemStack{ItemID: stone, Count: 10}}
	var outcome Outcome

	session.Click(view, inv, protocol.ClickModeCollect, 0, 0, false, 0, &outcome)

	if got := chest[0]; got.Count != 20 {
		t.Errorf("chest slot 0 = %+v, want the twenty left untouched", got)
	}
	if got := chest[1]; got.Count != 30 {
		t.Errorf("chest slot 1 = %+v, want the thirty left untouched", got)
	}
	if got := session.Carried.Count; got != 10 {
		t.Errorf("carried = %d, want the ten it held", got)
	}
}

// TestCollectWithAnEmptyCursorDoesNothing checks the cursor rule: a gather
// with nothing on the cursor is a no-op, even when the clicked slot holds
// the item and other slots hold more of it.
func TestCollectWithAnEmptyCursorDoesNothing(t *testing.T) {
	stone := itemID(t, "minecraft:stone")

	chest := make(slots, 27)
	chest[0] = protocol.ItemStack{ItemID: stone, Count: 20}
	chest[1] = protocol.ItemStack{ItemID: stone, Count: 30}
	view := View{Kind: KindChest, Entity: chest}
	inv := &player.Inventory{}
	session := &Session{}
	var outcome Outcome

	session.Click(view, inv, protocol.ClickModeCollect, 0, 0, false, 0, &outcome)

	if got := chest[0]; got.Count != 20 {
		t.Errorf("chest slot 0 = %+v, want the twenty left in place", got)
	}
	if got := chest[1]; got.Count != 30 {
		t.Errorf("chest slot 1 = %+v, want the thirty left in place", got)
	}
	if got := session.Carried; !got.Empty() {
		t.Errorf("carried = %+v, want the cursor left empty", got)
	}
}

// TestCollectReversedSweepStartsFromTheLastSlot checks the gather the client
// sends with button 1: the sweep walks from the last slot back, so the
// partial stacks nearest the end are drained first and the full stack at the
// last slot is only opened for the remainder.
func TestCollectReversedSweepStartsFromTheLastSlot(t *testing.T) {
	stone := itemID(t, "minecraft:stone")

	chest := make(slots, 27)
	chest[0] = protocol.ItemStack{ItemID: stone, Count: 40}
	chest[1] = protocol.ItemStack{ItemID: stone, Count: 40}
	chest[2] = protocol.ItemStack{ItemID: stone, Count: 64}
	view := View{Kind: KindChest, Entity: chest}
	inv := &player.Inventory{}
	session := &Session{Carried: protocol.ItemStack{ItemID: stone, Count: 10}}
	var outcome Outcome

	session.Click(view, inv, protocol.ClickModeCollect, 1, 5, false, 0, &outcome)

	// Reversed, slot 1 drains before slot 0, so slot 1 is emptied and the
	// twenty-six it could not take stay in slot 0.
	if got := chest[0]; got.Count != 26 {
		t.Errorf("chest slot 0 = %+v, want the twenty-six the reversed sweep left", got)
	}
	if got := chest[1]; !got.Empty() {
		t.Errorf("chest slot 1 = %+v, want it drained into the cursor", got)
	}
	if got := chest[2]; got.Count != 64 {
		t.Errorf("chest slot 2 = %+v, want the full stack untouched", got)
	}
	if got := session.Carried.Count; got != 64 {
		t.Errorf("carried = %d, want the whole stack the gather filled", got)
	}
}
