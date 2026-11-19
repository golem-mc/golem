package inventory

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
)

// The enchanting table's window has two slots of its own ahead of the
// player's things: slot 0 the item the table enchants, slot 1 its lapis.
// The tests here check the two slots' rules.

// enchantView is the table's window over the given two slots.
func enchantView(item, lapis protocol.ItemStack) View {
	return View{Kind: KindEnchant, Menu: []protocol.ItemStack{item, lapis}}
}

// TestTheEnchantingItemSlotHoldsASingleItem checks the one-item cap of the
// slot the table enchants.
func TestTheEnchantingItemSlotHoldsASingleItem(t *testing.T) {
	diamondID := itemID(t, "minecraft:diamond")

	// The whole cursor's worth offered to the empty slot: one goes in.
	view := enchantView(protocol.ItemStack{}, protocol.ItemStack{})
	session := &Session{Carried: protocol.ItemStack{ItemID: diamondID, Count: 12}}
	var outcome Outcome
	session.Click(view, &player.Inventory{}, protocol.ClickModePickup, 0, 0, false, 0, &outcome)
	if got := view.Menu[0]; got.Count != 1 {
		t.Errorf("the item slot holds %d, want the one it can", got.Count)
	}
	if got := session.Carried.Count; got != 11 {
		t.Errorf("the cursor holds %d, want the eleven that did not fit", got)
	}
	if len(outcome.Announce) != 1 || outcome.Announce[0] != 0 {
		t.Errorf("announced %+v, want the item slot's own correction: [0]", outcome.Announce)
	}

	// A full slot and a full stack of the same on the cursor: nothing
	// moves.
	view = enchantView(protocol.ItemStack{ItemID: diamondID, Count: 1}, protocol.ItemStack{})
	session = &Session{Carried: protocol.ItemStack{ItemID: diamondID, Count: 64}}
	outcome = Outcome{}
	session.Click(view, &player.Inventory{}, protocol.ClickModePickup, 0, 0, false, 0, &outcome)
	if got := view.Menu[0].Count; got != 1 {
		t.Errorf("the item slot holds %d, want the one it had", got)
	}
	if got := session.Carried.Count; got != 64 {
		t.Errorf("the cursor holds %d, want all sixty-four still", got)
	}

	// An empty cursor picks the one item up whole.
	session = &Session{}
	session.Click(view, &player.Inventory{}, protocol.ClickModePickup, 0, 0, false, 0, &outcome)
	if got := session.Carried; got.Count != 1 {
		t.Errorf("carried = %+v, want the single item", got)
	}
	if !view.Menu[0].Empty() {
		t.Errorf("the item slot = %+v, want it empty", view.Menu[0])
	}
}

// TestTheEnchantingLapisSlotTakesOnlyLapis checks the lapis slot's rule:
// lapis rests there and nothing else does, and a full slot takes no
// more.
func TestTheEnchantingLapisSlotTakesOnlyLapis(t *testing.T) {
	lapisID := itemID(t, "minecraft:lapis_lazuli")
	lapis := protocol.ItemStack{ItemID: lapisID, Count: 5}
	stone := protocol.ItemStack{ItemID: itemID(t, "minecraft:stone"), Count: 5}

	// Stone stays on the cursor, and nothing is announced.
	view := enchantView(protocol.ItemStack{}, protocol.ItemStack{})
	session := &Session{Carried: stone}
	var outcome Outcome
	session.Click(view, &player.Inventory{}, protocol.ClickModePickup, 0, 1, false, 0, &outcome)
	if got := session.Carried.Count; got != 5 {
		t.Errorf("the cursor holds %d, want the stone still on it", got)
	}
	if got := view.Menu[1]; !got.Empty() {
		t.Errorf("the lapis slot = %+v, want it empty", got)
	}
	if len(outcome.Announce) != 0 {
		t.Errorf("announced %+v, want nothing moved", outcome.Announce)
	}

	// Lapis rests there, the whole stack of it.
	view = enchantView(protocol.ItemStack{}, protocol.ItemStack{})
	session = &Session{Carried: lapis}
	session.Click(view, &player.Inventory{}, protocol.ClickModePickup, 0, 1, false, 0, &outcome)
	if got := view.Menu[1]; got.Count != 5 {
		t.Errorf("the lapis slot holds %d, want the whole five", got.Count)
	}

	// A right-click split into a part-full lapis slot adds one.
	view = enchantView(protocol.ItemStack{}, lapis)
	session = &Session{Carried: lapis}
	session.Click(view, &player.Inventory{}, protocol.ClickModePickup, 1, 1, false, 0, &outcome)
	if got := view.Menu[1].Count; got != 6 {
		t.Errorf("the lapis slot holds %d, want the five and the one", got)
	}
	if got := session.Carried.Count; got != 4 {
		t.Errorf("the cursor holds %d, want the one that merged spent", got)
	}

	// A full slot takes no right-click either.
	view = enchantView(protocol.ItemStack{}, protocol.ItemStack{ItemID: lapisID, Count: 64})
	session = &Session{Carried: protocol.ItemStack{ItemID: lapisID, Count: 64}}
	session.Click(view, &player.Inventory{}, protocol.ClickModePickup, 1, 1, false, 0, &outcome)
	if got := session.Carried.Count; got != 64 {
		t.Errorf("the cursor holds %d, want all sixty-four still", got)
	}
	if got := view.Menu[1].Count; got != 64 {
		t.Errorf("the lapis slot holds %d, want the sixty-four it had", got)
	}
}

// The window slot the hotbar sits at in a container window: the table's
// two own slots ahead of the main twenty-seven, so it starts at 29.
const enchantHotbarSlot = 2 + (player.HotbarSlot - player.MainSlot)

// TestAShiftClickFeedsTheEnchantingSlotsOneAtATime checks the quick moves
// of the player's things into the table's own slots: the lapis shifts in
// whole, anything else lays one item in and keeps the rest.
func TestAShiftClickFeedsTheEnchantingSlotsOneAtATime(t *testing.T) {
	lapis := protocol.ItemStack{ItemID: itemID(t, "minecraft:lapis_lazuli"), Count: 5}
	diamonds := protocol.ItemStack{ItemID: itemID(t, "minecraft:diamond"), Count: 7}
	inv := &player.Inventory{}
	inv.Set(player.HotbarSlot, lapis)
	inv.Set(player.HotbarSlot+1, diamonds)

	// The lapis shifts in whole.
	view := enchantView(protocol.ItemStack{}, protocol.ItemStack{})
	session := &Session{}
	var outcome Outcome
	session.Click(view, inv, protocol.ClickModeQuickMove, 0, enchantHotbarSlot, false, 0, &outcome)
	if got := view.Menu[1]; got.Count != 5 {
		t.Errorf("the lapis slot holds %d, want the whole five", got.Count)
	}
	if got := inv.Slots[player.HotbarSlot]; !got.Empty() {
		t.Errorf("the hotbar slot = %+v, want it spent", got)
	}

	// A shift of something else lays one item in, the rest stays in the
	// hotbar.
	session.Click(view, inv, protocol.ClickModeQuickMove, 0, enchantHotbarSlot+1, false, 0, &outcome)
	if got := view.Menu[0]; got.Count != 1 {
		t.Errorf("the item slot holds %d, want the one it can", got.Count)
	}
	if got := inv.Slots[player.HotbarSlot+1].Count; got != 6 {
		t.Errorf("the hotbar holds %d, want the six that stayed", got)
	}

	// A shift with the item slot full leaves everything where it was.
	session.Click(view, inv, protocol.ClickModeQuickMove, 0, enchantHotbarSlot+1, false, 0, &outcome)
	if got := view.Menu[0].Count; got != 1 {
		t.Errorf("the item slot holds %d, want the one it had", got)
	}
	if got := inv.Slots[player.HotbarSlot+1].Count; got != 6 {
		t.Errorf("the hotbar holds %d, want the six kept", got)
	}
}

// TestAHotbarSwapOverTheEnchantingItemSlotSpillsTheRest checks the number
// swap of a stack too many for the table's one-item slot: as much goes in
// as the slot holds, the rest stays on the hotbar, and what the slot held
// leaves for the player's things.
func TestAHotbarSwapOverTheEnchantingItemSlotSpillsTheRest(t *testing.T) {
	sword := protocol.ItemStack{ItemID: itemID(t, "minecraft:diamond_sword"), Count: 1}
	diamonds := protocol.ItemStack{ItemID: itemID(t, "minecraft:diamond"), Count: 64}

	// Sixty-four diamonds swapped in: 1 stays, 63 go home.
	inv := &player.Inventory{}
	inv.Set(player.HotbarSlot, diamonds)
	view := enchantView(protocol.ItemStack{}, protocol.ItemStack{})
	session := &Session{}
	var outcome Outcome
	session.Click(view, inv, protocol.ClickModeSwap, 0, 0, false, 0, &outcome)
	if got := view.Menu[0]; got.Count != 1 {
		t.Errorf("the item slot holds %d, want the one it can", got.Count)
	}
	if got := inv.Slots[player.HotbarSlot].Count; got != 63 {
		t.Errorf("the hotbar holds %d, want the sixty-three that stayed", got)
	}

	// A hotbar stack under the limit swaps whole.
	inv.Set(player.HotbarSlot+1, sword)
	view = enchantView(diamonds, protocol.ItemStack{})
	session.Click(view, inv, protocol.ClickModeSwap, 1, 0, false, 0, &outcome)
	if got := view.Menu[0]; !got.Equal(sword) {
		t.Errorf("the item slot = %+v, want the sword swapped in", got)
	}
	if got := inv.Slots[player.HotbarSlot+1]; !got.Equal(diamonds) {
		t.Errorf("the hotbar = %+v, want the diamonds swapped out", got)
	}

	// A stack too many for a slot already holding something: the item slot
	// takes one, the hotbar keeps the rest, and what the slot held falls in
	// among the player's things.
	inv = &player.Inventory{}
	inv.Set(player.HotbarSlot, diamonds)
	view = enchantView(sword, protocol.ItemStack{})
	session = &Session{}
	outcome = Outcome{}
	session.Click(view, inv, protocol.ClickModeSwap, 0, 0, false, 0, &outcome)
	if got := view.Menu[0]; got.Count != 1 || got.ItemID != diamonds.ItemID {
		t.Errorf("the item slot = %+v, want the one diamond", got)
	}
	if got := inv.Slots[player.HotbarSlot].Count; got != 63 {
		t.Errorf("the hotbar holds %d, want the sixty-three that stayed", got)
	}
	if got := inv.Slots[player.HotbarSlot+1]; !got.Equal(sword) {
		t.Errorf("the slot the sword fell to = %+v, want the sword", got)
	}
}
