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

// TestRecomputeCartographyRemakesTheResultFromTheInputs pins the recompute
// behind the take and the input changes: what the three recipes of the two
// inputs make, the answer stored whenever it differs from the stored one
// and never otherwise, away from the recipes nothing.
func TestRecomputeCartographyRemakesTheResultFromTheInputs(t *testing.T) {
	maps := cartographyMap(t, 3)
	zoomed := cartographyMarkedMap(t, 1, protocol.MapPostProcessingScale)
	locked := cartographyMarkedMap(t, 1, protocol.MapPostProcessingLock)
	two := maps
	two.Count = 2

	// Paper marks the map for the zoom out, a glass pane for the lock,
	// and an empty map clones: the two maps the take hands back.
	paper := protocol.ItemStack{ItemID: itemID(t, "minecraft:paper"), Count: 1}
	pane := protocol.ItemStack{ItemID: itemID(t, "minecraft:glass_pane"), Count: 1}
	emptyMap := protocol.ItemStack{ItemID: itemID(t, "minecraft:map"), Count: 1}
	for _, tc := range []struct {
		additional protocol.ItemStack
		want       protocol.ItemStack
	}{
		{paper, zoomed},
		{pane, locked},
		{emptyMap, two},
	} {
		menu := []protocol.ItemStack{maps, tc.additional, protocol.ItemStack{}}
		if !RecomputeCartography(menu, nil) || !menu[CartographyResultSlot].Equal(tc.want) {
			t.Errorf("a map beside %d made %+v, want %+v", tc.additional.ItemID,
				menu[CartographyResultSlot], tc.want)
		}
		// The same inputs answer the same result: nothing new is stored.
		if RecomputeCartography(menu, nil) {
			t.Errorf("the replayed recompute stored %+v, want it left as it was",
				menu[CartographyResultSlot])
		}
	}

	// Either input missing shows nothing, an addition its recipes do not
	// ask for too, and nothing but a filled map is a map for it: each
	// cleared from a stored answer, and never a new one made.
	menu := []protocol.ItemStack{maps, paper, zoomed}
	menu[1] = protocol.ItemStack{}
	if !RecomputeCartography(menu, nil) || !menu[CartographyResultSlot].Empty() {
		t.Errorf("the paper gone kept the answer %+v, want it cleared", menu[CartographyResultSlot])
	}
	menu = []protocol.ItemStack{maps, paper, zoomed}
	menu[1] = dirt(t)
	if !RecomputeCartography(menu, nil) || !menu[CartographyResultSlot].Empty() {
		t.Errorf("a map beside dirt kept the answer %+v, want it cleared", menu[CartographyResultSlot])
	}
	menu = []protocol.ItemStack{maps, paper, zoomed}
	menu[0] = protocol.ItemStack{}
	if !RecomputeCartography(menu, nil) || !menu[CartographyResultSlot].Empty() {
		t.Errorf("the map gone kept the answer %+v, want it cleared", menu[CartographyResultSlot])
	}
	menu = []protocol.ItemStack{
		{ItemID: itemID(t, "minecraft:map"), Count: 1}, paper, zoomed,
	}
	if !RecomputeCartography(menu, nil) || !menu[CartographyResultSlot].Empty() {
		t.Errorf("an empty map in the map slot answered %+v, want nothing", menu[CartographyResultSlot])
	}
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

// TestARightClickOnTheCartographyCloneTakesBothMaps checks the plain
// take's right button: the result container's removeItem ignores the
// amount asked of it, so a right click on the two-map clone hands both
// over onto an empty cursor, the two inputs spent behind it.
// vanilla: AbstractContainerMenu.doClick PICKUP, ResultContainer.removeItem
func TestARightClickOnTheCartographyCloneTakesBothMaps(t *testing.T) {
	maps := cartographyMap(t, 1)
	emptyMap := protocol.ItemStack{ItemID: itemID(t, "minecraft:map"), Count: 1}
	clone := cartographyMap(t, 2)

	view := cartographyView(maps, emptyMap, clone)
	session := &Session{}
	var outcome Outcome
	session.Click(view, &player.Inventory{}, protocol.ClickModePickup, 1, CartographyResultSlot, false, 0, &outcome)
	if got := session.Carried; !got.Equal(clone) {
		t.Errorf("carried = %+v, want both cloned maps", got)
	}
	if !view.Menu[0].Empty() || !view.Menu[1].Empty() {
		t.Errorf("the inputs = %+v, want both spent", view.Menu[:CartographyResultSlot])
	}
	if !view.Menu[CartographyResultSlot].Empty() {
		t.Errorf("the result = %+v, want nothing left to make", view.Menu[CartographyResultSlot])
	}
	if !outcome.CartographyTaken {
		t.Error("the take was not marked, want its sound paid for")
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

// TestTheCartographySlotsTakeOnlyWhatItsRecipesAsk checks the slots' own
// gate: the map slot a stack with a map id alone, the addition the paper, the
// empty map and the glass pane, and the result nothing at all.
// vanilla: CartographyTableMenu's slots' mayPlace
func TestTheCartographySlotsTakeOnlyWhatItsRecipesAsk(t *testing.T) {
	view := cartographyView(protocol.ItemStack{}, protocol.ItemStack{}, protocol.ItemStack{})

	mapItem := cartographyMap(t, 1)
	if !view.placeable(0, mapItem) {
		t.Error("a filled map with an id may not rest in the map slot, want it taken")
	}
	// Any stack with a map id is a map for the slot, an explorer map too.
	explorer := protocol.ItemStack{ItemID: itemID(t, "minecraft:woodland_mansion_map"), Count: 1}
	explorer.SetMapID(0)
	if !view.placeable(0, explorer) {
		t.Error("an explorer map with an id may not rest in the map slot, want it taken")
	}
	for _, refused := range []protocol.ItemStack{
		{ItemID: itemID(t, "minecraft:filled_map"), Count: 1},
		{ItemID: itemID(t, "minecraft:map"), Count: 1},
		{ItemID: itemID(t, "minecraft:paper"), Count: 1},
		{ItemID: itemID(t, "minecraft:dirt"), Count: 1},
	} {
		if view.placeable(0, refused) {
			t.Errorf("an item of ID %d may rest in the map slot, want it refused", refused.ItemID)
		}
	}
	for _, accepted := range []protocol.ItemStack{
		{ItemID: itemID(t, "minecraft:paper"), Count: 1},
		{ItemID: itemID(t, "minecraft:map"), Count: 1},
		{ItemID: itemID(t, "minecraft:glass_pane"), Count: 1},
	} {
		if !view.placeable(1, accepted) {
			t.Errorf("an item of ID %d may not rest in the addition, want one of the recipes' three",
				accepted.ItemID)
		}
	}
	for _, refused := range []protocol.ItemStack{
		{ItemID: itemID(t, "minecraft:filled_map"), Count: 1},
		{ItemID: itemID(t, "minecraft:dirt"), Count: 1},
	} {
		if view.placeable(1, refused) {
			t.Errorf("an item of ID %d may rest in the addition, want it refused", refused.ItemID)
		}
	}
	if view.placeable(CartographyResultSlot, mapItem) {
		t.Error("the result slot takes a map, want the slot that only gives")
	}
	if !view.placeable(cartographyHotbarSlot, mapItem) {
		t.Error("the player's own slots refused the map behind the table")
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

// TestAShiftClickOnTheCartographyResultMakesAMapPerPiece checks the
// result's quick-move: the answer lands in the player's things hotbar-end
// first once per recipe, for as long as the result lands somewhere and the
// inputs have a piece to spend, so the shift-click spends the whole pair.
func TestAShiftClickOnTheCartographyResultMakesAMapPerPiece(t *testing.T) {
	maps := cartographyMap(t, 2)
	paper := protocol.ItemStack{ItemID: itemID(t, "minecraft:paper"), Count: 2}
	zoomed := cartographyMarkedMap(t, 1, protocol.MapPostProcessingScale)

	inv := &player.Inventory{}
	view := cartographyView(maps, paper, zoomed)
	session := &Session{}
	var outcome Outcome
	session.Click(view, inv, protocol.ClickModeQuickMove, 0, CartographyResultSlot, false, 0, &outcome)
	// Both takes of the one click merge into the hotbar's farthest stack,
	// the vanilla move filling it whole; the marks the offers carried are
	// gone by the time they land.
	if got := inv.Slots[player.HotbarSlot+8]; !got.Equal(maps) {
		t.Errorf("the hotbar's end = %+v, want the two plain maps", got)
	}
	if !view.Menu[0].Empty() || !view.Menu[1].Empty() {
		t.Errorf("the inputs = %+v, want both spent by the click", view.Menu[:CartographyResultSlot])
	}
	if !view.Menu[CartographyResultSlot].Empty() {
		t.Errorf("the result = %+v, want nothing left to make", view.Menu[CartographyResultSlot])
	}
	if !outcome.CartographyTaken {
		t.Error("the takes were not marked, want the one sound of the click")
	}
}

// TestANumberKeyTakesTheCartographyResultWhole checks the swap, a number
// key over the result: the whole of it into an empty hotbar slot, one of
// each input spent behind it - and into a taken slot none of it, the
// result giving and never receiving.
func TestANumberKeyTakesTheCartographyResultWhole(t *testing.T) {
	maps := cartographyMap(t, 2)
	paper := protocol.ItemStack{ItemID: itemID(t, "minecraft:paper"), Count: 1}
	mapOne := maps
	mapOne.Count = 1
	zoomed := cartographyMarkedMap(t, 1, protocol.MapPostProcessingScale)

	inv := &player.Inventory{}
	view := cartographyView(maps, paper, zoomed)
	session := &Session{}
	var outcome Outcome
	session.Click(view, inv, protocol.ClickModeSwap, 0, CartographyResultSlot, false, 0, &outcome)
	if got := inv.Slots[player.HotbarSlot]; !got.Equal(mapOne) {
		t.Errorf("the hotbar = %+v, want the result's whole plain map", got)
	}
	if got := view.Menu[0]; !got.Equal(mapOne) {
		t.Errorf("the map = %+v, want the swap's take to spend a piece", got)
	}
	if got := view.Menu[1]; !got.Empty() {
		t.Errorf("the addition = %+v, want its last piece spent", got)
	}
	if !outcome.CartographyTaken {
		t.Error("the swap's take was not marked, want its sound paid for")
	}

	// A hotbar slot already holding something takes nothing back, the
	// result being no slot to swap into.
	dirt := protocol.ItemStack{ItemID: itemID(t, "minecraft:dirt"), Count: 1}
	inv = &player.Inventory{}
	inv.Set(player.HotbarSlot, dirt)
	view = cartographyView(maps, paper, zoomed)
	outcome = Outcome{}
	session.Click(view, inv, protocol.ClickModeSwap, 0, CartographyResultSlot, false, 0, &outcome)
	if got := inv.Slots[player.HotbarSlot]; !got.Equal(dirt) {
		t.Errorf("the hotbar = %+v, want the dirt left in it", got)
	}
	if got := view.Menu[CartographyResultSlot]; !got.Equal(zoomed) {
		t.Errorf("the result = %+v, want the answer still whole", got)
	}
}

// TestTheDropKeyThrowsTheCartographyResultOut checks the Q's take: the
// whole offer for a plain Q - the result container's removeItem ignoring
// the amount a plain Q asks for - and, for Ctrl+Q, the whole offer once
// per piece the inputs have, the inputs spent behind every throw.
func TestTheDropKeyThrowsTheCartographyResultOut(t *testing.T) {
	maps := cartographyMap(t, 2)
	paper := protocol.ItemStack{ItemID: itemID(t, "minecraft:paper"), Count: 2}
	zoomed := cartographyMarkedMap(t, 1, protocol.MapPostProcessingScale)
	// A clone's answer: the two plain maps the cloning take hands back.
	clone := cartographyMap(t, 2)
	oneMap := maps
	oneMap.Count = 1

	// The plain Q drops the whole offer the result holds, cloning
	// included - the result container's removeItem ignoring the one the
	// plain Q asks for.
	view := cartographyView(oneMap, protocol.ItemStack{ItemID: itemID(t, "minecraft:map"), Count: 1}, clone)
	session := &Session{}
	var outcome Outcome
	session.Click(view, &player.Inventory{}, protocol.ClickModeThrow, 0, CartographyResultSlot, false, 0, &outcome)
	if len(outcome.Dropped) != 1 || !outcome.Dropped[0].Equal(clone) {
		t.Errorf("dropped %v, want the plain Q's whole offer", outcome.Dropped)
	}
	if !view.Menu[0].Empty() || !view.Menu[1].Empty() {
		t.Errorf("the inputs = %+v, want both spent by the throw", view.Menu[:CartographyResultSlot])
	}
	if !view.Menu[CartographyResultSlot].Empty() {
		t.Errorf("the result = %+v, want the throwing over", view.Menu[CartographyResultSlot])
	}

	// Ctrl+Q throws the whole offer once per piece the inputs have: two
	// pieces of each behind two answer throws.
	view = cartographyView(maps, paper, zoomed)
	outcome = Outcome{}
	session.Click(view, &player.Inventory{}, protocol.ClickModeThrow, 1, CartographyResultSlot, false, 0, &outcome)
	if len(outcome.Dropped) != 2 || !outcome.Dropped[0].Equal(oneMap) || !outcome.Dropped[1].Equal(oneMap) {
		t.Errorf("dropped %v, want two whole-offer throws", outcome.Dropped)
	}
	if !view.Menu[0].Empty() || !view.Menu[1].Empty() {
		t.Errorf("the inputs = %+v, want both spent by the throws", view.Menu[:CartographyResultSlot])
	}
	if !view.Menu[CartographyResultSlot].Empty() {
		t.Errorf("the result = %+v, want nothing left to throw", view.Menu[CartographyResultSlot])
	}
}

// TestADoubleClickNeverGathersTheCartographyResult checks the sweep's
// exclusion: a double click on another slot, the cursor holding the
// result's own map, leaves the result container alone.
// vanilla: CartographyTableMenu.canTakeItemForPickAll
func TestADoubleClickNeverGathersTheCartographyResult(t *testing.T) {
	maps := cartographyMap(t, 2)
	paper := protocol.ItemStack{ItemID: itemID(t, "minecraft:paper"), Count: 2}
	zoomed := cartographyMarkedMap(t, 1, protocol.MapPostProcessingScale)

	inv := &player.Inventory{}
	view := cartographyView(maps, paper, zoomed)
	session := &Session{Carried: zoomed}
	var outcome Outcome
	session.Click(view, inv, protocol.ClickModeCollect, 0, cartographyHotbarSlot, false, 0, &outcome)
	if got := session.Carried; !got.Equal(zoomed) {
		t.Errorf("carried = %+v, want the marked map left alone", got)
	}
	if got := view.Menu[CartographyResultSlot]; !got.Equal(zoomed) {
		t.Errorf("the result = %+v, want the answer still whole", got)
	}
	if !view.Menu[0].Equal(maps) || !view.Menu[1].Equal(paper) {
		t.Errorf("the inputs = %+v, want both whole behind the sweep", view.Menu[:CartographyResultSlot])
	}
	if outcome.CartographyTaken {
		t.Error("the sweep took from the result, want it excluded")
	}
}

// TestClosingTheCartographyWindowHandsBackItsInputs checks the close: the
// map and the addition go home to the player, the answer the result was
// showing goes with the window, never handed back as a stack of its own.
func TestClosingTheCartographyWindowHandsBackItsInputs(t *testing.T) {
	maps := cartographyMap(t, 1)
	paper := protocol.ItemStack{ItemID: itemID(t, "minecraft:paper"), Count: 2}
	zoomed := cartographyMarkedMap(t, 1, protocol.MapPostProcessingScale)

	inv := &player.Inventory{}
	view := cartographyView(maps, paper, zoomed)
	session := &Session{}
	session.Close(view, inv, false, false)
	if got := inv.Slots[player.HotbarSlot]; !got.Equal(maps) {
		t.Errorf("the hotbar = %+v, want the input's map", got)
	}
	if got := inv.Slots[player.HotbarSlot+1]; !got.Equal(paper) {
		t.Errorf("the second hotbar slot = %+v, want the addition's paper", got)
	}
	for _, slot := range inv.Slots {
		if slot.Equal(zoomed) {
			t.Errorf("the inventory holds the result's answer %+v, want it to have gone with the window", slot)
		}
	}
}
