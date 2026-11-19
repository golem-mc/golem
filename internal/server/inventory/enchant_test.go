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

// The window slot the hotbar sits at in a container window: the table's
// two own slots ahead of the main twenty-seven, so it starts at 29.
const enchantHotbarSlot = 2 + (player.HotbarSlot - player.MainSlot)
