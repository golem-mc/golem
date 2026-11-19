package inventory

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
)

// The anvil's double click: the result is gathered from like any other
// slot, but the result slot takes nothing, so a take from it is the whole
// result or nothing - vanilla: Slot.tryRemove's allowModification. These
// drive the mode 6 click over an anvil whose result is five named dirt.

// namedDirt is count dirt under a custom name, the stack a rename makes of
// it.
func namedDirt(t *testing.T, name string, count int32) protocol.ItemStack {
	t.Helper()
	stack := protocol.ItemStack{ItemID: itemID(t, "minecraft:dirt"), Count: count}
	text := protocol.Text{Text: name}
	stack.CustomName = &text
	return stack
}

// anvilCollectView is the anvil that renames five plain dirt to "Bar", the
// result slot holding the stack the window recomputed.
func anvilCollectView(t *testing.T) View {
	t.Helper()
	base := protocol.ItemStack{ItemID: itemID(t, "minecraft:dirt"), Count: 5}
	return View{
		Kind:      KindAnvil,
		Menu:      []protocol.ItemStack{base, {}, namedDirt(t, "Bar", 5)},
		AnvilName: "Bar",
	}
}

// TestAPlainClickOnTheAnvilsResultTakesItWhole checks the plain take's
// button: the result container's removeItem ignores the amount a right
// click asks for, so both buttons hand the whole result over, the inputs
// cleared and the cost paid behind it.
// vanilla: AbstractContainerMenu.doClick PICKUP, ResultContainer.removeItem
func TestAPlainClickOnTheAnvilsResultTakesItWhole(t *testing.T) {
	for _, button := range []int32{0, 1} {
		view := anvilCollectView(t)
		session := &Session{}
		var outcome Outcome
		session.Click(view, &player.Inventory{}, protocol.ClickModePickup, button, AnvilResultSlot, false, 10, &outcome)
		if got := session.Carried; !got.Equal(namedDirt(t, "Bar", 5)) {
			t.Errorf("button %d carried = %+v, want the whole result", button, got)
		}
		if !view.Menu[0].Empty() {
			t.Errorf("button %d first input = %+v, want it cleared", button, view.Menu[0])
		}
		if !outcome.AnvilTaken {
			t.Errorf("button %d was not marked, want the cost paid", button)
		}
	}
}

// TestAnAnvilCollectTakesTheWholeResultOrNothing checks the result slot's
// part of a double click: a cursor of the same named dirt is only topped up
// when it has room for the whole five, and a cursor with a single slot of
// room for them is refused rather than given a part-take - Slot.tryRemove
// refuses a partial take from a slot that cannot be placed into.
func TestAnAnvilCollectTakesTheWholeResultOrNothing(t *testing.T) {
	t.Run("no room for the whole result", func(t *testing.T) {
		view := anvilCollectView(t)
		inv := &player.Inventory{}
		session := &Session{Carried: namedDirt(t, "Bar", 63)}
		var outcome Outcome

		session.Click(view, inv, protocol.ClickModeCollect, 0, 3, false, 10, &outcome)

		want := namedDirt(t, "Bar", 63)
		if got := session.Carried; !got.Equal(want) {
			t.Errorf("cursor = %+v, want the sixty-three left unjoined", got)
		}
		base := protocol.ItemStack{ItemID: itemID(t, "minecraft:dirt"), Count: 5}
		if got := view.Menu[0]; !got.Equal(base) {
			t.Errorf("the first input = %+v, want the five dirt untouched", got)
		}
		if got := view.Menu[AnvilResultSlot]; !got.Equal(namedDirt(t, "Bar", 5)) {
			t.Errorf("the result = %+v, want the whole result left", got)
		}
		if outcome.AnvilTaken {
			t.Error("the refused gather spent the anvil")
		}
	})

	t.Run("room for the whole result", func(t *testing.T) {
		view := anvilCollectView(t)
		inv := &player.Inventory{}
		session := &Session{Carried: namedDirt(t, "Bar", 58)}
		var outcome Outcome

		session.Click(view, inv, protocol.ClickModeCollect, 0, 3, false, 10, &outcome)

		want := namedDirt(t, "Bar", 63)
		if got := session.Carried; !got.Equal(want) {
			t.Errorf("cursor = %+v, want the whole result joined onto it", got)
		}
		if got := view.Menu[0]; !got.Empty() {
			t.Errorf("the first input = %+v, want the take to empty it", got)
		}
		if !outcome.AnvilTaken {
			t.Error("the gather did not spend the anvil")
		}
	})
}

// TestADoubleClickOnAnUnaffordableAnvilResultGathersAndKeepsIt checks that a
// result the player cannot pay for refuses the pickup, so the sweep gathers
// like on an empty slot and leaves the result and inputs alone.
// vanilla: AbstractContainerMenu.doClick PICKUP_ALL
func TestADoubleClickOnAnUnaffordableAnvilResultGathersAndKeepsIt(t *testing.T) {
	for _, tc := range []struct {
		name   string
		level  int32
		gather bool
	}{{"unaffordable", 0, true}, {"affordable", 10, false}} {
		t.Run(tc.name, func(t *testing.T) {
			view := anvilCollectView(t)
			inv := &player.Inventory{}
			inv.Slots[player.HotbarSlot] = namedDirt(t, "Bar", 4)
			session := &Session{Carried: namedDirt(t, "Bar", 1)}
			var outcome Outcome

			session.Click(view, inv, protocol.ClickModeCollect, 0, AnvilResultSlot, false, tc.level, &outcome)

			want, left := namedDirt(t, "Bar", 1), namedDirt(t, "Bar", 4)
			if tc.gather {
				want, left = namedDirt(t, "Bar", 5), protocol.ItemStack{}
			}
			if got := session.Carried; !got.Equal(want) {
				t.Errorf("cursor = %+v, want %+v", got, want)
			}
			if got := inv.Slots[player.HotbarSlot]; !got.Equal(left) {
				t.Errorf("hotbar slot = %+v, want %+v", got, left)
			}
			if got := view.Menu[AnvilResultSlot]; !got.Equal(namedDirt(t, "Bar", 5)) {
				t.Errorf("result = %+v, want it kept", got)
			}
			if outcome.AnvilTaken {
				t.Error("the gather spent the anvil")
			}
		})
	}
}
