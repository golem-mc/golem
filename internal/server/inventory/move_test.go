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

// TestShiftClickOutOfAFurnaceTakesTheResult checks the shift-click out of
// a furnace window: the stack lands in the player's things, the output is
// emptied, and the move is marked as the take the furnace's stored
// experience is paid on.
func TestShiftClickOutOfAFurnaceTakesTheResult(t *testing.T) {
	ingots := protocol.ItemStack{ItemID: itemID(t, "minecraft:iron_ingot"), Count: 10}
	furnace := make(slots, 3)
	furnace[2] = ingots
	inv := &player.Inventory{}
	var outcome Outcome
	session := &Session{}
	session.Click(View{Kind: KindFurnace, ID: 3, Entity: furnace}, inv,
		protocol.ClickModeQuickMove, 0, 2, false, 0, &outcome)

	if got := inv.Slots[player.HotbarSlot+8]; !got.Equal(ingots) {
		t.Errorf("the last hotbar slot = %+v, want the ingots from the furnace", got)
	}
	if !furnace[2].Empty() {
		t.Errorf("furnace slot 2 = %+v, want it emptied by the move", furnace[2])
	}
	if !outcome.ResultTaken {
		t.Error("the shift-click out of the output was not marked as a take")
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

// TestShiftClickFallsBackToTheMainHotbarSwap checks a stack the window has
// nowhere to put moves from the main inventory to the hotbar, and back.
// vanilla: AbstractFurnaceMenu.quickMoveStack, CraftingMenu.quickMoveStack
func TestShiftClickFallsBackToTheMainHotbarSwap(t *testing.T) {
	dirt := protocol.ItemStack{ItemID: itemID(t, "minecraft:dirt"), Count: 5}
	stone := protocol.ItemStack{ItemID: itemID(t, "minecraft:stone"), Count: 64}

	views := map[string]func() (View, int32){
		"furnace":       func() (View, int32) { return View{Kind: KindFurnace, Entity: make(slots, 3)}, 3 },
		"blast furnace": func() (View, int32) { return View{Kind: KindBlastFurnace, Entity: make(slots, 3)}, 3 },
		"smoker":        func() (View, int32) { return View{Kind: KindSmoker, Entity: make(slots, 3)}, 3 },
		"crafting": func() (View, int32) {
			grid := make([]protocol.ItemStack, CraftingGridCells)
			for i := range grid {
				grid[i] = stone
			}
			return View{Kind: KindCrafting, Grid: grid}, 10
		},
	}
	for name, mk := range views {
		t.Run(name, func(t *testing.T) {
			view, base := mk()
			inv := &player.Inventory{}
			inv.Set(player.MainSlot, dirt)
			var outcome Outcome
			(&Session{}).Click(view, inv, protocol.ClickModeQuickMove, 0, base, false, 0, &outcome)
			if got := inv.Slots[player.HotbarSlot]; !got.Equal(dirt) {
				t.Errorf("first hotbar slot = %+v, want the dirt from the main inventory", got)
			}
			if got := inv.Slots[player.MainSlot]; !got.Empty() {
				t.Errorf("main inventory slot 9 = %+v, want it emptied", got)
			}

			view, base = mk()
			inv = &player.Inventory{}
			inv.Set(player.HotbarSlot, dirt)
			outcome = Outcome{}
			(&Session{}).Click(view, inv, protocol.ClickModeQuickMove, 0, base+player.MainInvSlots-player.HotbarSlots, false, 0, &outcome)
			if got := inv.Slots[player.MainSlot]; !got.Equal(dirt) {
				t.Errorf("main inventory slot 9 = %+v, want the dirt from the hotbar", got)
			}
		})
	}
}

// TestAFurnaceOutputIsTakenWholeOrNotAtAll checks that neither the double
// click nor a pickup onto a cursor of the same item takes part of the
// output: the slot cannot be placed into, so the take is the whole stack
// or nothing.
// vanilla: Slot.tryRemove, Slot.allowModification
func TestAFurnaceOutputIsTakenWholeOrNotAtAll(t *testing.T) {
	ingot := itemID(t, "minecraft:iron_ingot")
	cases := []struct {
		name        string
		mode        int32
		button      int32
		slot        int32
		cursor, out int32
		wantCursor  int32
		wantOut     int32
		wantTaken   bool
	}{
		{"gather without room", protocol.ClickModeCollect, 0, 0, 40, 30, 40, 30, false},
		{"gather with room", protocol.ClickModeCollect, 0, 0, 30, 30, 60, 0, true},
		{"left pickup without room", protocol.ClickModePickup, 0, 2, 40, 30, 40, 30, false},
		{"right pickup without room", protocol.ClickModePickup, 1, 2, 40, 30, 40, 30, false},
		{"left pickup with room", protocol.ClickModePickup, 0, 2, 30, 30, 60, 0, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			furnace := make(slots, 3)
			furnace[2] = protocol.ItemStack{ItemID: ingot, Count: c.out}
			session := &Session{Carried: protocol.ItemStack{ItemID: ingot, Count: c.cursor}}
			var outcome Outcome
			session.Click(View{Kind: KindFurnace, ID: 3, Entity: furnace}, &player.Inventory{},
				c.mode, c.button, c.slot, false, 0, &outcome)
			if session.Carried.Count != c.wantCursor || furnace[2].Count != c.wantOut {
				t.Errorf("cursor %d, output %d; want %d and %d",
					session.Carried.Count, furnace[2].Count, c.wantCursor, c.wantOut)
			}
			if outcome.ResultTaken != c.wantTaken {
				t.Errorf("ResultTaken = %v, want %v", outcome.ResultTaken, c.wantTaken)
			}
		})
	}
}

// TestCraftingGridRestMovesToTheHotbar shift-clicks a stack the grid takes
// only part of: the repeat pass sends the rest from the main inventory to
// the hotbar.
// vanilla: AbstractContainerMenu.doClick (QUICK_MOVE), CraftingMenu.quickMoveStack
func TestCraftingGridRestMovesToTheHotbar(t *testing.T) {
	dirt := itemID(t, "minecraft:dirt")
	stone := protocol.ItemStack{ItemID: itemID(t, "minecraft:stone"), Count: 64}
	grid := make([]protocol.ItemStack, CraftingGridCells)
	for i := range grid {
		grid[i] = stone
	}
	grid[0] = protocol.ItemStack{ItemID: dirt, Count: 60}
	inv := &player.Inventory{}
	inv.Set(player.MainSlot, protocol.ItemStack{ItemID: dirt, Count: 64})
	var outcome Outcome
	(&Session{}).Click(View{Kind: KindCrafting, Grid: grid}, inv, protocol.ClickModeQuickMove, 0, 10, false, 0, &outcome)
	if want := (protocol.ItemStack{ItemID: dirt, Count: 64}); !grid[0].Equal(want) {
		t.Errorf("grid cell = %+v, want 64 dirt", grid[0])
	}
	if want := (protocol.ItemStack{ItemID: dirt, Count: 60}); !inv.Slots[player.HotbarSlot].Equal(want) {
		t.Errorf("first hotbar slot = %+v, want the 60 dirt the grid had no room for", inv.Slots[player.HotbarSlot])
	}
	if got := inv.Slots[player.MainSlot]; !got.Empty() {
		t.Errorf("main inventory slot 9 = %+v, want it emptied", got)
	}
}
