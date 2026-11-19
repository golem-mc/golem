package inventory

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
)

// The anvil window's quick moves: a shift-clicked stack lays into the
// inputs, the first that takes it.

// anvilView is the anvil's window over the given inputs, the result slot's
// store behind them.
func anvilView(first, second protocol.ItemStack) View {
	return View{Kind: KindAnvil, Menu: []protocol.ItemStack{first, second, {}}}
}

// anvilHotbarSlot is the window slot the hotbar sits at in the anvil: the
// two inputs and the result ahead of the main twenty-seven.
const anvilHotbarSlot = AnvilResultSlot + 1 + (player.HotbarSlot - player.MainSlot)

// TestAShiftClickFeedsAnAnvilsFirstInputThatFits checks the routing of a
// shift-click out of the player's things: into the first input when it is
// empty, the second only when the first is taken.
func TestAShiftClickFeedsAnAnvilsFirstInputThatFits(t *testing.T) {
	book := protocol.ItemStack{ItemID: itemID(t, "minecraft:enchanted_book"), Count: 1}
	sword := protocol.ItemStack{ItemID: itemID(t, "minecraft:diamond_sword"), Count: 1}

	// Both inputs empty: the first takes the whole stack.
	inv := &player.Inventory{}
	inv.Set(player.HotbarSlot, book)
	view := anvilView(protocol.ItemStack{}, protocol.ItemStack{})
	var outcome Outcome
	session := &Session{}
	session.Click(view, inv, protocol.ClickModeQuickMove, 0, anvilHotbarSlot, false, 0, &outcome)
	if !view.Menu[0].Equal(book) {
		t.Errorf("the first input = %+v, want the book", view.Menu[0])
	}
	if got := inv.Slots[player.HotbarSlot]; !got.Empty() {
		t.Errorf("the hotbar = %+v, want it emptied by the move", got)
	}

	// The first input taken: the second takes it, the first left alone.
	inv = &player.Inventory{}
	inv.Set(player.HotbarSlot, book)
	view = anvilView(sword, protocol.ItemStack{})
	outcome = Outcome{}
	session = &Session{}
	session.Click(view, inv, protocol.ClickModeQuickMove, 0, anvilHotbarSlot, false, 0, &outcome)
	if !view.Menu[1].Equal(book) {
		t.Errorf("the second input = %+v, want the book", view.Menu[1])
	}
	if !view.Menu[0].Equal(sword) {
		t.Errorf("the first input = %+v, want the sword untouched", view.Menu[0])
	}
	if got := inv.Slots[player.HotbarSlot]; !got.Empty() {
		t.Errorf("the hotbar = %+v, want it emptied by the move", got)
	}
}
