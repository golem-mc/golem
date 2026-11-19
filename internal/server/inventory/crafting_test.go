package inventory

import (
	"slices"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
)

// TestCraftFromThePlayerGridConsumesWhatItMatched drives the 2x2 of the
// player's own window a click at a time: two planks stacked one above the
// other are worth four sticks, and taking the sticks consumes the pair.
func TestCraftFromThePlayerGridConsumesWhatItMatched(t *testing.T) {
	planks := protocol.ItemStack{ItemID: itemID(t, "minecraft:oak_planks"), Count: 2}
	button := protocol.ItemStack{ItemID: itemID(t, "minecraft:oak_button"), Count: 1}
	sticks := protocol.ItemStack{ItemID: itemID(t, "minecraft:stick"), Count: 4}

	inv := &player.Inventory{}
	inv.Set(player.HotbarSlot, planks)
	session := &Session{}
	view := View{}

	// The whole stack into the hand, then one plank into each of slots 1 and 3.
	var outcome Outcome
	session.Click(view, inv, protocol.ClickModePickup, 0, player.HotbarSlot, false, 0, &outcome)
	if got := session.Carried; !got.Equal(planks) {
		t.Fatalf("carried = %+v after the pickup, want the two planks", got)
	}
	session.Click(view, inv, protocol.ClickModePickup, 1, playerCraftingGrid, false, 0, &outcome)
	if got := inv.Slots[playerCraftingGrid]; !got.Equal(protocol.ItemStack{ItemID: planks.ItemID, Count: 1}) {
		t.Fatalf("grid cell %d = %+v, want one plank placed", playerCraftingGrid, got)
	}
	// One plank alone is worth what the data pack says it makes.
	if got := view.Stack(inv, playerCraftingResult); !got.Equal(button) {
		t.Errorf("the result of one plank = %+v, want %+v (an oak button)", got, button)
	}
	if !slices.Contains(outcome.Announce, playerCraftingResult) {
		t.Errorf("announced %+v, want the result slot among them", outcome.Announce)
	}

	// The second plank lands under the first, and the column is sticks.
	session.Click(view, inv, protocol.ClickModePickup, 1, playerCraftingGrid+2, false, 0, &outcome)
	if got := view.Stack(inv, playerCraftingResult); !got.Equal(sticks) {
		t.Fatalf("the result of the column = %+v, want %+v (the sticks)", got, sticks)
	}

	// Taking the result consumes the pair: both cells empty, the cursor
	// holding the sticks.
	outcome = Outcome{}
	session.Click(view, inv, protocol.ClickModePickup, 0, playerCraftingResult, false, 0, &outcome)
	if got := session.Carried; !got.Equal(sticks) {
		t.Errorf("carried = %+v after taking the result, want %+v", got, sticks)
	}
	for _, slot := range []int32{playerCraftingGrid, playerCraftingGrid + 2} {
		if got := inv.Slots[slot]; !got.Empty() {
			t.Errorf("grid cell %d = %+v after the craft, want the plank consumed", slot, got)
		}
	}
	// The taking announces the whole grid and the result slot.
	for _, want := range []int32{
		playerCraftingResult,
		playerCraftingGrid, playerCraftingGrid + 1, playerCraftingGrid + 2, playerCraftingGrid + 3,
	} {
		if !slices.Contains(outcome.Announce, want) {
			t.Errorf("announced %+v, want slot %d among them (the craft's whole footprint)",
				outcome.Announce, want)
		}
	}
}
