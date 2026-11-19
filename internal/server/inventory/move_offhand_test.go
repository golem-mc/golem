package inventory

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
)

// TestShiftClickStowsShieldInTheOffHand checks the off-hand's share of the
// shift-click routing. A shield's equippable component names the off hand
// while setting swappable to false, but the vanilla InventoryMenu's
// quickMoveStack goes by the slot alone, so a shift-clicked shield still
// tries the off hand first.
func TestShiftClickStowsShieldInTheOffHand(t *testing.T) {
	shield := protocol.ItemStack{ItemID: itemID(t, "minecraft:shield"), Count: 1}
	stone := protocol.ItemStack{ItemID: itemID(t, "minecraft:stone"), Count: 1}

	// A shield in the main inventory goes straight into the empty off
	// hand.
	inv := &player.Inventory{}
	inv.Set(player.MainSlot, shield)
	var outcome Outcome
	session := &Session{}
	session.Click(View{}, inv, protocol.ClickModeQuickMove, 0, player.MainSlot, false, 0, &outcome)
	if got := inv.Slots[player.OffHandSlot]; !got.Equal(shield) {
		t.Errorf("the off hand holds %+v, want the shield shifted into it", got)
	}
	if !inv.Slots[player.MainSlot].Empty() {
		t.Errorf("main inventory slot 9 = %+v, want it emptied by the move", inv.Slots[player.MainSlot])
	}
	if !outcome.Visible {
		t.Error("a shift-click that stowed the off hand was not marked for the equipment broadcast")
	}

	// The off hand taken, the shield falls in among the rest of the
	// things like any other item: nothing of it is forced onto the stone.
	inv = &player.Inventory{}
	inv.Set(player.OffHandSlot, stone)
	inv.Set(player.HotbarSlot, shield)
	outcome = Outcome{}
	session = &Session{}
	session.Click(View{}, inv, protocol.ClickModeQuickMove, 0, player.HotbarSlot, false, 0, &outcome)
	if got := inv.Slots[player.MainSlot]; !got.Equal(shield) {
		t.Errorf("main inventory slot 9 = %+v, want the shield the taken off hand left", got)
	}
	if got := inv.Slots[player.OffHandSlot]; !got.Equal(stone) {
		t.Errorf("the off hand holds %+v, want the stone untouched", got)
	}

	// And a shield already stowed moves the other way, into the main
	// inventory and the hotbar, the way the armour slots' stacks do.
	inv = &player.Inventory{}
	inv.Set(player.OffHandSlot, shield)
	outcome = Outcome{}
	session = &Session{}
	session.Click(View{}, inv, protocol.ClickModeQuickMove, 0, player.OffHandSlot, false, 0, &outcome)
	if got := inv.Slots[player.MainSlot]; !got.Equal(shield) {
		t.Errorf("main inventory slot 9 = %+v, want the shield shifted out of the off hand", got)
	}
	if !inv.Slots[player.OffHandSlot].Empty() {
		t.Errorf("the off hand holds %+v, want it emptied by the move", inv.Slots[player.OffHandSlot])
	}
}
