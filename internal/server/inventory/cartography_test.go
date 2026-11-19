package inventory

import (
	"slices"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
)

// The cartography window over the given map, addition and the result
// slot's stored truth, laid out the way the server's input-change hooks
// leave them between the clicks it recomputes for.

// cartographyView is the cartography table's window over its two inputs
// and the result slot's stored truth.
func cartographyView(mapStack, additional, result protocol.ItemStack) View {
	return View{Kind: KindCartography, Menu: []protocol.ItemStack{mapStack, additional, result}}
}

// cartographyHotbarSlot is where the hotbar sits in the cartography
// table's window: the two inputs and the result ahead of the main
// twenty-seven.
const cartographyHotbarSlot = CartographyResultSlot + 1 + (player.HotbarSlot - player.MainSlot)

// cartographyMap builds a filled map of map id 0, the table's map slot
// taking only a stack with an id.
func cartographyMap(t *testing.T, count int32) protocol.ItemStack {
	t.Helper()
	s := protocol.ItemStack{ItemID: itemID(t, "minecraft:filled_map"), Count: count}
	s.SetMapID(0)
	return s
}

// cartographyMarkedMap builds a filled map marked for one of the table's
// two marks, the stack its zoom out and lock results hold.
func cartographyMarkedMap(t *testing.T, count int32, mark int32) protocol.ItemStack {
	t.Helper()
	s := cartographyMap(t, count)
	s.SetMapPostProcessing(mark)
	return s
}

// dirt builds a dirt stack, the item no cartography recipe asks for.
func dirt(t *testing.T) protocol.ItemStack {
	t.Helper()
	return protocol.ItemStack{ItemID: itemID(t, "minecraft:dirt"), Count: 1}
}

// TestTheCartographyTakeSpendsOneOfEachInput checks the take behind every
// mode that reaches the result: one of each input goes, the result keeps
// offering the same answer, and either input's last piece takes the
// result with it.
func TestTheCartographyTakeSpendsOneOfEachInput(t *testing.T) {
	maps := cartographyMap(t, 2)
	paper := protocol.ItemStack{ItemID: itemID(t, "minecraft:paper"), Count: 2}
	paperOne := paper
	paperOne.Count = 1
	mapOne := maps
	mapOne.Count = 1
	zoomed := cartographyMarkedMap(t, 1, protocol.MapPostProcessingScale)

	inv := &player.Inventory{}
	// The first take of a two-and-two input pair: one of each goes, and
	// the result still offers the same answer, unchecked by the take. The
	// map the take hands back carries no mark, the plain copy vanilla's
	// onCraftedBy leaves.
	view := cartographyView(maps, paper, zoomed)
	session := &Session{}
	var outcome Outcome
	session.Click(view, inv, protocol.ClickModePickup, 0, CartographyResultSlot, false, 0, &outcome)
	if got := session.Carried; !got.Equal(mapOne) {
		t.Errorf("carried = %+v, want the take's plain map", got)
	}
	if got := view.Menu[0]; !got.Equal(mapOne) {
		t.Errorf("the map = %+v, want one map of two", got)
	}
	if got := view.Menu[1]; !got.Equal(paperOne) {
		t.Errorf("the addition = %+v, want one paper of two", got)
	}
	if got := view.Menu[CartographyResultSlot]; !got.Equal(zoomed) {
		t.Errorf("the result = %+v, want the answer still on offer", got)
	}
	if !outcome.CartographyTaken {
		t.Error("the take was not marked, want its sound paid for")
	}

	// The map goes home, clearing the cursor: the plain map the take
	// handed back no longer matches the marked offer it came from.
	outcome = Outcome{}
	session.Click(view, inv, protocol.ClickModePickup, 0, cartographyHotbarSlot, false, 0, &outcome)
	if got := inv.Slots[player.HotbarSlot]; !got.Equal(mapOne) {
		t.Errorf("the hotbar = %+v, want the taken map put away", got)
	}
	if !session.Carried.Empty() {
		t.Errorf("the cursor = %+v, want it emptied by the put-away", session.Carried)
	}

	// The second take spends the last of both, and the result slot goes
	// empty with them.
	outcome = Outcome{}
	session.Click(view, inv, protocol.ClickModePickup, 0, CartographyResultSlot, false, 0, &outcome)
	if got := session.Carried; !got.Equal(mapOne) {
		t.Errorf("carried = %+v, want the second take's map", got)
	}
	if !view.Menu[0].Empty() || !view.Menu[1].Empty() {
		t.Errorf("the inputs = %+v, want both spent", view.Menu[:CartographyResultSlot])
	}
	if !view.Menu[CartographyResultSlot].Empty() {
		t.Errorf("the result = %+v, want nothing left to make", view.Menu[CartographyResultSlot])
	}
	if !slices.Contains(outcome.Announce, 0) || !slices.Contains(outcome.Announce, 1) ||
		!slices.Contains(outcome.Announce, CartographyResultSlot) {
		t.Errorf("announced %v, want both inputs and the emptied result", outcome.Announce)
	}

	// With nothing to make the next take announces and spends nothing.
	outcome = Outcome{}
	session.Click(view, inv, protocol.ClickModePickup, 0, CartographyResultSlot, false, 0, &outcome)
	if got := session.Carried; !got.Equal(mapOne) {
		t.Errorf("carried = %+v, want the take to have given nothing", got)
	}
	if len(outcome.Announce) != 0 || outcome.CartographyTaken {
		t.Errorf("announced %v (taken %t), want a dead window", outcome.Announce, outcome.CartographyTaken)
	}
}

// TestTheCartographyTakeStripsTheMark checks that no map leaving the
// result slot carries the table's minecraft:map_post_processing mark:
// vanilla's MapItem.onCraftedBy strips it on the plain take, the
// shift-click, the swap, the throw and the creative clone alike, while
// the offer the slot holds keeps the mark until the take.
// vanilla: MapItem.onCraftedBy, CartographyTableMenu's result slot safeClone
func TestTheCartographyTakeStripsTheMark(t *testing.T) {
	maps := cartographyMap(t, 2)
	paper := protocol.ItemStack{ItemID: itemID(t, "minecraft:paper"), Count: 2}
	mapOne := maps
	mapOne.Count = 1
	zoomed := cartographyMarkedMap(t, 1, protocol.MapPostProcessingScale)

	// A plain take hands the unmarked map to the cursor, while the offer
	// the slot still shows keeps its mark.
	view := cartographyView(maps, paper, zoomed)
	session := &Session{}
	var outcome Outcome
	session.Click(view, &player.Inventory{}, protocol.ClickModePickup, 0, CartographyResultSlot, false, 0, &outcome)
	if got := session.Carried; !got.Equal(mapOne) {
		t.Errorf("the taken map = %+v, want the mark stripped", got)
	}
	if got := view.Menu[CartographyResultSlot]; !got.Equal(zoomed) {
		t.Errorf("the offer = %+v, want it still marked after the take", got)
	}

	// A shift-click lands the unmarked maps in the player's things,
	// merging into the plain stack the two takes' worth make.
	inv := &player.Inventory{}
	view = cartographyView(maps, paper, zoomed)
	session = &Session{}
	outcome = Outcome{}
	session.Click(view, inv, protocol.ClickModeQuickMove, 0, CartographyResultSlot, false, 0, &outcome)
	if got := inv.Slots[player.HotbarSlot+8]; !got.Equal(maps) {
		t.Errorf("the shifted maps = %+v, want the mark stripped", got)
	}

	// A number key puts the unmarked map into the hotbar.
	inv = &player.Inventory{}
	view = cartographyView(maps, paper, zoomed)
	session = &Session{}
	outcome = Outcome{}
	session.Click(view, inv, protocol.ClickModeSwap, 0, CartographyResultSlot, false, 0, &outcome)
	if got := inv.Slots[player.HotbarSlot]; !got.Equal(mapOne) {
		t.Errorf("the swapped map = %+v, want the mark stripped", got)
	}

	// The Q drops the unmarked map.
	view = cartographyView(maps, paper, zoomed)
	session = &Session{}
	outcome = Outcome{}
	session.Click(view, &player.Inventory{}, protocol.ClickModeThrow, 0, CartographyResultSlot, false, 0, &outcome)
	if len(outcome.Dropped) != 1 || !outcome.Dropped[0].Equal(mapOne) {
		t.Errorf("the dropped map = %v, want the mark stripped", outcome.Dropped)
	}

	// A creative clone copies the whole stack without the mark.
	full := maps
	full.Count = stackMax(maps)
	view = cartographyView(maps, paper, zoomed)
	session = &Session{}
	outcome = Outcome{}
	session.Click(view, &player.Inventory{}, protocol.ClickModeClone, 0, CartographyResultSlot, true, 0, &outcome)
	if got := session.Carried; !got.Equal(full) {
		t.Errorf("the cloned map = %+v, want the mark stripped", got)
	}
}

// TestAShiftClickRoutesTheCartographyTable checks the shift-click's
// routing: out of the player's things the map lays in the map slot, the
// paper, the empty map and the glass pane in the addition, the rest moves
// among their own rows, and the inputs home to the player's things.
// vanilla: CartographyTableMenu.quickMoveStack
func TestAShiftClickRoutesTheCartographyTable(t *testing.T) {
	maps := cartographyMap(t, 2)
	paper := protocol.ItemStack{ItemID: itemID(t, "minecraft:paper"), Count: 3}
	dirt := protocol.ItemStack{ItemID: itemID(t, "minecraft:dirt"), Count: 3}

	// The filled map lays whole in the map slot.
	inv := &player.Inventory{}
	inv.Set(player.HotbarSlot, maps)
	view := cartographyView(protocol.ItemStack{}, protocol.ItemStack{}, protocol.ItemStack{})
	var outcome Outcome
	session := &Session{}
	session.Click(view, inv, protocol.ClickModeQuickMove, 0, cartographyHotbarSlot, false, 0, &outcome)
	if got := view.Menu[0]; !got.Equal(maps) {
		t.Errorf("the map slot = %+v, want the whole stack of maps", got)
	}
	if got := inv.Slots[player.HotbarSlot]; !got.Empty() {
		t.Errorf("the hotbar = %+v, want it emptied by the move", got)
	}

	// The paper lays in the addition, the empty map and the pane among
	// them among the same move.
	for _, addition := range []protocol.ItemStack{paper,
		{ItemID: itemID(t, "minecraft:map"), Count: 3},
		{ItemID: itemID(t, "minecraft:glass_pane"), Count: 3}} {
		inv = &player.Inventory{}
		inv.Set(player.HotbarSlot, addition)
		view = cartographyView(protocol.ItemStack{}, protocol.ItemStack{}, protocol.ItemStack{})
		outcome = Outcome{}
		session.Click(view, inv, protocol.ClickModeQuickMove, 0, cartographyHotbarSlot, false, 0, &outcome)
		if got := view.Menu[1]; !got.Equal(addition) {
			t.Errorf("an item of ID %d moved to %+v, want the addition taking them whole",
				addition.ItemID, view.Menu[1])
		}
		if got := inv.Slots[player.HotbarSlot]; !got.Empty() {
			t.Errorf("an item of ID %d stayed in the hotbar, want the move to have taken it",
				addition.ItemID)
		}
	}

	// The map slot taken, a map shifted into the window goes nowhere - and
	// does not fall through to the player's own rows.
	inv = &player.Inventory{}
	inv.Set(player.HotbarSlot, maps)
	view = cartographyView(maps, protocol.ItemStack{}, protocol.ItemStack{})
	outcome = Outcome{}
	session.Click(view, inv, protocol.ClickModeQuickMove, 0, cartographyHotbarSlot, false, 0, &outcome)
	if got := inv.Slots[player.MainSlot]; !got.Empty() {
		t.Errorf("the main rows took %+v, want the map left in the hotbar", got)
	}

	// What neither input takes moves among the player's own rows.
	inv = &player.Inventory{}
	inv.Set(player.HotbarSlot, dirt)
	view = cartographyView(protocol.ItemStack{}, protocol.ItemStack{}, protocol.ItemStack{})
	outcome = Outcome{}
	session.Click(view, inv, protocol.ClickModeQuickMove, 0, cartographyHotbarSlot, false, 0, &outcome)
	if got := inv.Slots[player.MainSlot]; !got.Equal(dirt) {
		t.Errorf("the main rows = %+v, want the dirt among them", got)
	}
	if got := view.Menu[0]; !got.Empty() {
		t.Errorf("the map slot did take %+v, want the dirt never laid", got)
	}

	// The inputs home to the player's things, the main rows first.
	inv = &player.Inventory{}
	view = cartographyView(maps, paper, protocol.ItemStack{})
	outcome = Outcome{}
	session.Click(view, inv, protocol.ClickModeQuickMove, 0, 1, false, 0, &outcome)
	if got := inv.Slots[player.MainSlot]; !got.Equal(paper) {
		t.Errorf("the main rows = %+v, want the addition's paper", got)
	}
	if !view.Menu[1].Empty() {
		t.Errorf("the addition = %+v, want it shifted home", view.Menu[1])
	}
}
