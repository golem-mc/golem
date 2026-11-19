package inventory

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/registry"
)

// The grindstone window over the given inputs and the result, laid out
// the way the server's half of the menu leaves them between the clicks it
// recomputes for.

// grindstoneView is the grindstone's window over the two inputs and the
// result slot's stored truth.
func grindstoneView(input0, input1, result protocol.ItemStack) View {
	return View{Kind: KindGrindstone, Menu: []protocol.ItemStack{input0, input1, result}}
}

// grindstoneHotbarSlot is where the hotbar sits in the grindstone's
// window: the two inputs and the result ahead of the main twenty-seven.
const grindstoneHotbarSlot = GrindstoneResultSlot + 1 + (player.HotbarSlot - player.MainSlot)

// grindstoneEnchantedStack builds a stack of one enchanted item or book,
// the enchantment named at the level given.
func grindstoneEnchantedStack(t *testing.T, item, enchantment string, level int32) protocol.ItemStack {
	t.Helper()
	enchantID, ok := registry.EnchantmentID(enchantment)
	if !ok {
		t.Fatalf("the enchantment registry has no %s", enchantment)
	}
	s := protocol.ItemStack{ItemID: itemID(t, item), Count: 1}
	if s.ItemID == itemID(t, "minecraft:enchanted_book") {
		s.StoredEnchantments = map[int32]int32{enchantID: level}
		return s
	}
	s.Enchantments = map[int32]int32{enchantID: level}
	return s
}

// TestTheGrindstonesTakeEmptiesBothInputsAndCarriesTheWorth checks the
// take behind every mode that reaches the result: whichever stacks the
// inputs hold go with it, and the worth their magic strips off rides the
// outcome for the caller to pay.
func TestTheGrindstonesTakeEmptiesBothInputsAndCarriesTheWorth(t *testing.T) {
	sword := grindstoneEnchantedStack(t, "minecraft:diamond_sword", "minecraft:sharpness", 1)
	pick := grindstoneEnchantedStack(t, "minecraft:diamond_pickaxe", "minecraft:efficiency", 1)
	stripped := protocol.ItemStack{ItemID: itemID(t, "minecraft:diamond_sword"), Count: 1}

	view := grindstoneView(sword, pick, stripped)
	session := &Session{}
	var outcome Outcome
	session.Click(view, &player.Inventory{}, protocol.ClickModePickup, 0, GrindstoneResultSlot, false, 0, &outcome)
	if got := session.Carried; !got.Equal(stripped) {
		t.Errorf("carried = %+v, want the result's whole stack", got)
	}
	if got := view.Menu[0]; !got.Empty() {
		t.Errorf("the first input = %+v, want it emptied by the take", got)
	}
	if got := view.Menu[1]; !got.Empty() {
		t.Errorf("the second input = %+v, want it emptied by the take", got)
	}
	if !outcome.GrindstoneTaken {
		t.Error("the take was not marked, want its worth paid for")
	}
	// Sharpness I's and efficiency I's minimum costs, one each, summed
	// over both inputs.
	if outcome.GrindstoneWorth != 2 {
		t.Errorf("the take's worth = %d, want one a magic each", outcome.GrindstoneWorth)
	}

	// A window emptied by its take serves nothing further, the answer
	// it spent kept on the cursor.
	outcome = Outcome{}
	session.Click(view, &player.Inventory{}, protocol.ClickModePickup, 0, GrindstoneResultSlot, false, 0, &outcome)
	if got := session.Carried; !got.Equal(stripped) {
		t.Errorf("carried = %+v, want the emptied result to have given nothing", got)
	}
	if outcome.GrindstoneTaken || outcome.GrindstoneWorth != 0 {
		t.Errorf("taken %t worth %d, want a dead window", outcome.GrindstoneTaken, outcome.GrindstoneWorth)
	}
}

// TestAShiftClickFeedsTheGrindstonesInputs checks the shift-click's
// routing: the player's things lay into whichever input is free, what
// the grindstone refuses stays where it was - the inputs being the whole
// of that move - both inputs taken leaving the move among the player's
// own rows, and the inputs home to them.
// vanilla: GrindstoneMenu.quickMoveStack
func TestAShiftClickFeedsTheGrindstonesInputs(t *testing.T) {
	sword := protocol.ItemStack{ItemID: itemID(t, "minecraft:diamond_sword"), Count: 1}
	stone := protocol.ItemStack{ItemID: itemID(t, "minecraft:stone"), Count: 3}

	// Both inputs free: the whole stack lays in the first.
	inv := &player.Inventory{}
	inv.Set(player.HotbarSlot, sword)
	view := grindstoneView(protocol.ItemStack{}, protocol.ItemStack{}, protocol.ItemStack{})
	var outcome Outcome
	session := &Session{}
	session.Click(view, inv, protocol.ClickModeQuickMove, 0, grindstoneHotbarSlot, false, 0, &outcome)
	if got := view.Menu[0]; !got.Equal(sword) {
		t.Errorf("the first input = %+v, want the whole stack of the sword", got)
	}
	if got := inv.Slots[player.HotbarSlot]; !got.Empty() {
		t.Errorf("the hotbar = %+v, want it emptied by the move", got)
	}

	// The first taken: the second takes the stack, the first left alone.
	inv = &player.Inventory{}
	inv.Set(player.HotbarSlot, sword)
	view = grindstoneView(sword, protocol.ItemStack{}, protocol.ItemStack{})
	outcome = Outcome{}
	session = &Session{}
	session.Click(view, inv, protocol.ClickModeQuickMove, 0, grindstoneHotbarSlot, false, 0, &outcome)
	if got := view.Menu[1]; !got.Equal(sword) {
		t.Errorf("the second input = %+v, want the sword", got)
	}
	if !view.Menu[0].Equal(sword) {
		t.Errorf("the first input = %+v, want it left alone", view.Menu[0])
	}

	// Both inputs taken: the move runs among the player's own rows.
	inv = &player.Inventory{}
	inv.Set(player.HotbarSlot, sword)
	view = grindstoneView(sword, sword, protocol.ItemStack{})
	outcome = Outcome{}
	session = &Session{}
	session.Click(view, inv, protocol.ClickModeQuickMove, 0, grindstoneHotbarSlot, false, 0, &outcome)
	if got := inv.Slots[player.MainSlot]; !got.Equal(sword) {
		t.Errorf("the main rows = %+v, want the sword moved into them", got)
	}
	if got := view.Menu[0]; !got.Equal(sword) {
		t.Errorf("the first input = %+v, want nothing laid without a free slot", got)
	}

	// What the inputs refuse stays where it was, the inputs being the
	// whole of the move with a slot free.
	inv = &player.Inventory{}
	inv.Set(player.HotbarSlot, stone)
	view = grindstoneView(protocol.ItemStack{}, protocol.ItemStack{}, protocol.ItemStack{})
	outcome = Outcome{}
	session = &Session{}
	session.Click(view, inv, protocol.ClickModeQuickMove, 0, grindstoneHotbarSlot, false, 0, &outcome)
	if got := view.Menu[0]; !got.Empty() {
		t.Errorf("the first input = %+v, want the stone never laid", got)
	}
	if got := inv.Slots[player.HotbarSlot]; !got.Equal(stone) {
		t.Errorf("the hotbar = %+v, want the stone to have stayed", got)
	}

	// An input home to the player's things, the main rows first.
	inv = &player.Inventory{}
	view = grindstoneView(sword, protocol.ItemStack{}, protocol.ItemStack{})
	outcome = Outcome{}
	session = &Session{}
	session.Click(view, inv, protocol.ClickModeQuickMove, 0, 0, false, 0, &outcome)
	if got := inv.Slots[player.MainSlot]; !got.Equal(sword) {
		t.Errorf("the main rows = %+v, want the input's sword", got)
	}
	if !view.Menu[0].Empty() {
		t.Errorf("the first input = %+v, want it shifted home", view.Menu[0])
	}
}
