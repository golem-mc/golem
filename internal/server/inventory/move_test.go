package inventory

import (
	"slices"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
)

// TestShiftClickRoutesBetweenTheWindowsSections checks the routing of the
// shift-click, which is every window's own decision about where a stack
// goes: a chest hands the player's things back from the hotbar's far end, a
// player's own window moves between its main inventory and hotbar, and a
// furnace tries the slot the item belongs in.
func TestShiftClickRoutesBetweenTheWindowsSections(t *testing.T) {
	apples := protocol.ItemStack{ItemID: itemID(t, "minecraft:apple"), Count: 10}
	stone := protocol.ItemStack{ItemID: itemID(t, "minecraft:stone"), Count: 64}
	pearls := protocol.ItemStack{ItemID: itemID(t, "minecraft:ender_pearl"), Count: 16}
	diamonds := protocol.ItemStack{ItemID: itemID(t, "minecraft:diamond"), Count: 3}
	rawIron := protocol.ItemStack{ItemID: itemID(t, "minecraft:raw_iron"), Count: 1}
	coal := protocol.ItemStack{ItemID: itemID(t, "minecraft:coal"), Count: 1}

	// A chest's slot lands in the player's things counted from the
	// hotbar's far end: nothing to merge with and a window of 63 slots,
	// so the first empty from the back is window slot 62.
	chest := make(slots, 27)
	chest[3] = apples
	inv := &player.Inventory{}
	var outcome Outcome
	session := &Session{}
	session.Click(View{Kind: KindChest, ID: 2, Entity: chest}, inv,
		protocol.ClickModeQuickMove, 0, 3, false, 0, &outcome)
	if got := inv.Slots[player.HotbarSlot+8]; !got.Equal(apples) {
		t.Errorf("the last hotbar slot = %+v, want the apples from the chest", got)
	}
	if !chest[3].Empty() {
		t.Errorf("chest slot 3 = %+v, want it emptied by the move", chest[3])
	}
	if !slices.Contains(outcome.Announce, 3) || !slices.Contains(outcome.Announce, 62) {
		t.Errorf("announced %+v, want the chest's slot 3 and the hotbar's 62", outcome.Announce)
	}
	if !slices.Contains(outcome.EntitySlots, 3) {
		t.Errorf("the other viewers were not told the chest's own slot changed: %+v",
			outcome.EntitySlots)
	}

	// The player's things land in the chest's first free slot, counted from
	// the front: window slot 27 is the main inventory's first, so it lands
	// in the chest's slot 0.
	chest = make(slots, 27)
	inv = &player.Inventory{}
	inv.Set(player.MainSlot, stone)
	outcome = Outcome{}
	session = &Session{}
	session.Click(View{Kind: KindChest, ID: 2, Entity: chest}, inv,
		protocol.ClickModeQuickMove, 0, 27, false, 0, &outcome)
	if !chest[0].Equal(stone) {
		t.Errorf("chest slot 0 = %+v, want the stone the player moved in", chest[0])
	}
	if !inv.Slots[player.MainSlot].Empty() {
		t.Errorf("main inventory slot 9 = %+v, want it emptied by the move", inv.Slots[player.MainSlot])
	}

	// In the player's own window, each section moves to the other.
	inv = &player.Inventory{}
	inv.Set(player.HotbarSlot, pearls)
	outcome = Outcome{}
	session = &Session{}
	session.Click(View{}, inv, protocol.ClickModeQuickMove, 0, player.HotbarSlot, false, 0, &outcome)
	if got := inv.Slots[player.MainSlot]; !got.Equal(pearls) {
		t.Errorf("main inventory slot 9 = %+v, want the pearls moved in from the hotbar", got)
	}

	inv = &player.Inventory{}
	inv.Set(player.MainSlot, diamonds)
	outcome = Outcome{}
	session = &Session{}
	session.Click(View{}, inv, protocol.ClickModeQuickMove, 0, player.MainSlot, false, 0, &outcome)
	if got := inv.Slots[player.HotbarSlot]; !got.Equal(diamonds) {
		t.Errorf("hotbar slot 36 = %+v, want the diamonds moved in from the main inventory", got)
	}

	// The furnace tries where the item belongs.
	furnace := make(slots, 3)
	inv = &player.Inventory{}
	inv.Set(player.MainSlot, rawIron)
	outcome = Outcome{}
	session = &Session{}
	session.Click(View{Kind: KindFurnace, ID: 3, Entity: furnace}, inv,
		protocol.ClickModeQuickMove, 0, 3, false, 0, &outcome)
	if !furnace[0].Equal(rawIron) {
		t.Errorf("furnace ingredient slot = %+v, want the raw iron", furnace[0])
	}

	furnace = make(slots, 3)
	inv = &player.Inventory{}
	inv.Set(player.MainSlot, coal)
	outcome = Outcome{}
	session = &Session{}
	session.Click(View{Kind: KindFurnace, ID: 3, Entity: furnace}, inv,
		protocol.ClickModeQuickMove, 0, 3, false, 0, &outcome)
	if !furnace[1].Equal(coal) {
		t.Errorf("furnace fuel slot = %+v, want the coal", furnace[1])
	}

}

// TestShiftClickOutOfACookingWindowMovesInputAndFuelForwards checks which
// end of the player's things the three cooking windows' shift-click fills:
// the ingredient and the fuel walk forwards from window slot 3, the first
// main-inventory slot, and only the output walks backwards from the hotbar's
// far end. vanilla: AbstractFurnaceMenu.quickMoveStack
func TestShiftClickOutOfACookingWindowMovesInputAndFuelForwards(t *testing.T) {
	for _, tc := range []struct {
		name string
		kind Kind
	}{
		{"furnace", KindFurnace},
		{"blast furnace", KindBlastFurnace},
		{"smoker", KindSmoker},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// The ingredient slot 0 and the fuel slot 1: an empty
			// inventory's first main-inventory slot is window slot 3.
			for _, in := range []struct {
				slot  int32
				stack protocol.ItemStack
			}{
				{0, protocol.ItemStack{ItemID: itemID(t, "minecraft:raw_iron"), Count: 1}},
				{1, protocol.ItemStack{ItemID: itemID(t, "minecraft:coal"), Count: 5}},
			} {
				furnace := make(slots, 3)
				furnace[in.slot] = in.stack
				inv := &player.Inventory{}
				var outcome Outcome
				session := &Session{}
				session.Click(View{Kind: tc.kind, ID: 3, Entity: furnace}, inv,
					protocol.ClickModeQuickMove, 0, in.slot, false, 0, &outcome)
				if got := inv.Slots[player.MainSlot]; !got.Equal(in.stack) {
					t.Errorf("slot %d: the first main-inventory slot = %+v, want %+v sent forwards",
						in.slot, got, in.stack)
				}
				if !furnace[in.slot].Empty() {
					t.Errorf("slot %d = %+v, want it emptied by the move", in.slot, furnace[in.slot])
				}
			}

			// The output slot 2 still empties into the hotbar's far end.
			ingots := protocol.ItemStack{ItemID: itemID(t, "minecraft:iron_ingot"), Count: 3}
			furnace := make(slots, 3)
			furnace[2] = ingots
			inv := &player.Inventory{}
			var outcome Outcome
			session := &Session{}
			session.Click(View{Kind: tc.kind, ID: 3, Entity: furnace}, inv,
				protocol.ClickModeQuickMove, 0, 2, false, 0, &outcome)
			if got := inv.Slots[player.HotbarSlot+player.HotbarSlots-1]; !got.Equal(ingots) {
				t.Errorf("the output = %+v, want %+v sent backwards into the last hotbar slot",
					got, ingots)
			}
		})
	}
}
