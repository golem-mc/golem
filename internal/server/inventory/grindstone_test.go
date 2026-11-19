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

// TestAPlainClickOnTheGrindstonesResultTakesItWhole checks the plain
// take's button: the result container's removeItem ignores the amount a
// right click asks for, so both buttons hand the whole answer over, the
// inputs emptied behind it.
// vanilla: AbstractContainerMenu.doClick PICKUP, ResultContainer.removeItem
func TestAPlainClickOnTheGrindstonesResultTakesItWhole(t *testing.T) {
	book := protocol.ItemStack{ItemID: itemID(t, "minecraft:book"), Count: 1}
	two := protocol.ItemStack{ItemID: itemID(t, "minecraft:book"), Count: 2}

	for _, button := range []int32{0, 1} {
		view := grindstoneView(book, book, two)
		session := &Session{}
		var outcome Outcome
		session.Click(view, &player.Inventory{}, protocol.ClickModePickup, button, GrindstoneResultSlot, false, 0, &outcome)
		if got := session.Carried; !got.Equal(two) {
			t.Errorf("button %d carried = %+v, want both books", button, got)
		}
		if !view.Menu[0].Empty() || !view.Menu[1].Empty() {
			t.Errorf("button %d inputs = %+v, want both emptied", button, view.Menu[:2])
		}
		if !outcome.GrindstoneTaken {
			t.Errorf("button %d was not marked, want its worth paid for", button)
		}
	}
}

// TestRecomputeGrindstoneRemakesTheResultFromTheInputs pins the
// recompute's own rule: the inputs' answer is stored whenever it differs
// from the stored one, and whenever force is set - the take's empties
// written even where the answer was already empty.
func TestRecomputeGrindstoneRemakesTheResultFromTheInputs(t *testing.T) {
	sword := grindstoneEnchantedStack(t, "minecraft:diamond_sword", "minecraft:sharpness", 1)
	stripped := protocol.ItemStack{ItemID: sword.ItemID, Count: 1}

	menu := []protocol.ItemStack{sword, protocol.ItemStack{}, protocol.ItemStack{}}
	if stored := RecomputeGrindstone(menu, false); !stored || !menu[GrindstoneResultSlot].Equal(stripped) {
		t.Errorf("recomputed %v (stored %t), want the stripped sword", menu[GrindstoneResultSlot], stored)
	}
	// The same inputs answer the same result: nothing new is stored.
	if stored := RecomputeGrindstone(menu, false); stored {
		t.Errorf("the replayed recompute stored %v, want it left as it was", menu[GrindstoneResultSlot])
	}
	// An input emptied against a stored answer clears it.
	menu[0] = protocol.ItemStack{}
	if !RecomputeGrindstone(menu, false) || !menu[GrindstoneResultSlot].Empty() {
		t.Errorf("the emptied inputs kept the result %v, want it cleared", menu[GrindstoneResultSlot])
	}
	// The forced recompute stores even an unchanged answer.
	if stored := RecomputeGrindstone(menu, true); !stored {
		t.Error("the forced recompute stored nothing, want the empties written")
	}
}

// TestTheGrindstonesInputsTakeOnlyWhatItWorksOn checks the input slots'
// own gate: what a grindstone can work on - an item that wears or one
// carrying magic, in either component - and nothing else; the result
// takes nothing at all.
// vanilla: GrindstoneMenu's input slots' mayPlace
func TestTheGrindstonesInputsTakeOnlyWhatItWorksOn(t *testing.T) {
	view := grindstoneView(protocol.ItemStack{}, protocol.ItemStack{}, protocol.ItemStack{})

	plain := protocol.ItemStack{ItemID: itemID(t, "minecraft:diamond_pickaxe"), Count: 1}
	if !view.placeable(0, plain) {
		t.Error("a plain pickaxe may not rest in the input, want an item that wears taken")
	}
	if !view.placeable(0, grindstoneEnchantedStack(t, "minecraft:enchanted_book", "minecraft:sharpness", 1)) {
		t.Error("an enchanted book may not rest in the input, want its magic taken")
	}
	for _, refused := range []protocol.ItemStack{
		{ItemID: itemID(t, "minecraft:stone"), Count: 1},
		{ItemID: itemID(t, "minecraft:book"), Count: 1},
		{ItemID: itemID(t, "minecraft:dirt"), Count: 1},
	} {
		if view.placeable(0, refused) {
			t.Errorf("a stack of %d (the item of ID %d) may rest in the input, want it refused",
				refused.Count, refused.ItemID)
		}
	}
	if view.placeable(GrindstoneResultSlot, plain) {
		t.Error("the result slot takes a pickaxe, want the slot that only gives")
	}
	if !view.placeable(grindstoneHotbarSlot, plain) {
		t.Error("the player's own slots refused the pickaxe behind the grindstone")
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

// TestAShiftClickThatFitsNowhereSpendsNoInputs checks the result's
// quick-move when the player's things have no room: nothing moves, so the
// take never runs and both inputs stay.
// vanilla: GrindstoneMenu.quickMoveStack
func TestAShiftClickThatFitsNowhereSpendsNoInputs(t *testing.T) {
	sword := grindstoneEnchantedStack(t, "minecraft:diamond_sword", "minecraft:sharpness", 1)
	stripped := protocol.ItemStack{ItemID: sword.ItemID, Count: 1}
	stone := protocol.ItemStack{ItemID: itemID(t, "minecraft:stone"), Count: 64}

	inv := &player.Inventory{}
	for slot := player.MainSlot; slot < player.MainSlot+player.MainInvSlots; slot++ {
		inv.Set(slot, stone)
	}
	view := grindstoneView(sword, protocol.ItemStack{}, stripped)
	session := &Session{}
	var outcome Outcome
	session.Click(view, inv, protocol.ClickModeQuickMove, 0, GrindstoneResultSlot, false, 0, &outcome)
	if !view.Menu[0].Equal(sword) {
		t.Errorf("the first input = %+v, want it left whole with nowhere to move", view.Menu[0])
	}
	if !view.Menu[GrindstoneResultSlot].Equal(stripped) {
		t.Errorf("the result = %+v, want it left whole", view.Menu[GrindstoneResultSlot])
	}
	if outcome.GrindstoneTaken {
		t.Error("the take ran over a move that moved nothing, want no take")
	}
}

// TestANumberKeyTakesTheGrindstonesResultWhole checks the swap, a number
// key over the result: the whole of it into an empty hotbar slot, its
// take emptying the inputs - and into a taken slot none of it, the
// result giving and never receiving.
func TestANumberKeyTakesTheGrindstonesResultWhole(t *testing.T) {
	sword := protocol.ItemStack{ItemID: itemID(t, "minecraft:diamond_sword"), Count: 1}
	stripped := sword

	inv := &player.Inventory{}
	view := grindstoneView(sword, protocol.ItemStack{}, stripped)
	session := &Session{}
	var outcome Outcome
	session.Click(view, inv, protocol.ClickModeSwap, 0, GrindstoneResultSlot, false, 0, &outcome)
	if got := inv.Slots[player.HotbarSlot]; !got.Equal(stripped) {
		t.Errorf("the hotbar = %+v, want the result's whole stack", got)
	}
	if got := view.Menu[0]; !got.Empty() {
		t.Errorf("the first input = +%+v, want the swap's take to have emptied it", got)
	}
	if !outcome.GrindstoneTaken {
		t.Error("the swap's take was not marked, want its worth paid for")
	}

	// A hotbar slot already holding something takes nothing back, the
	// result being no slot to swap into.
	dirt := protocol.ItemStack{ItemID: itemID(t, "minecraft:dirt"), Count: 1}
	inv = &player.Inventory{}
	inv.Set(player.HotbarSlot, dirt)
	view = grindstoneView(sword, protocol.ItemStack{}, stripped)
	outcome = Outcome{}
	session.Click(view, inv, protocol.ClickModeSwap, 0, GrindstoneResultSlot, false, 0, &outcome)
	if got := inv.Slots[player.HotbarSlot]; !got.Equal(dirt) {
		t.Errorf("the hotbar = %+v, want the dirt left in it", got)
	}
	if got := view.Menu[GrindstoneResultSlot]; !got.Equal(stripped) {
		t.Errorf("the result = %+v, want its answer still whole", got)
	}
}

// TestTheDropKeyThrowsTheGrindstonesResultOut checks the Q's take: the whole
// answer for a plain Q and for Ctrl+Q, both with the inputs emptied behind
// them.
// vanilla: ResultContainer.removeItem
func TestTheDropKeyThrowsTheGrindstonesResultOut(t *testing.T) {
	sword := protocol.ItemStack{ItemID: itemID(t, "minecraft:diamond_sword"), Count: 1}
	two := protocol.ItemStack{ItemID: itemID(t, "minecraft:book"), Count: 2}

	// The plain Q drops the whole result too.
	view := grindstoneView(sword, protocol.ItemStack{}, two)
	session := &Session{}
	var outcome Outcome
	session.Click(view, &player.Inventory{}, protocol.ClickModeThrow, 0, GrindstoneResultSlot, false, 0, &outcome)
	if len(outcome.Dropped) != 1 || !outcome.Dropped[0].Equal(two) {
		t.Errorf("dropped %v, want the whole answer of the plain Q", outcome.Dropped)
	}
	if !view.Menu[0].Empty() || !view.Menu[1].Empty() {
		t.Errorf("the inputs = %+v, want them emptied by the throw", view.Menu[:2])
	}

	// Ctrl+Q throws the whole answer at once.
	view = grindstoneView(sword, protocol.ItemStack{}, two)
	outcome = Outcome{}
	session.Click(view, &player.Inventory{}, protocol.ClickModeThrow, 1, GrindstoneResultSlot, false, 0, &outcome)
	if len(outcome.Dropped) != 1 || !outcome.Dropped[0].Equal(two) {
		t.Errorf("dropped %v, want the whole answer thrown", outcome.Dropped)
	}
	if !view.Menu[0].Empty() || !view.Menu[1].Empty() {
		t.Errorf("the inputs = %+v, want them emptied by the throws", view.Menu[:2])
	}
}

// TestADoubleClickNeverGathersFromTheGrindstoneResultItself checks the
// collect against the grindstone: the sweep of a double click counts the
// result among its places, taking what the cursor has room for with the
// inputs' empties behind it, while a double click on the result itself
// gathers nothing.
// vanilla: AbstractContainerMenu.doClick PICKUP_ALL
func TestADoubleClickNeverGathersFromTheGrindstoneResultItself(t *testing.T) {
	sword := protocol.ItemStack{ItemID: itemID(t, "minecraft:diamond_sword"), Count: 1}
	book := protocol.ItemStack{ItemID: itemID(t, "minecraft:book"), Count: 1}
	held := protocol.ItemStack{ItemID: itemID(t, "minecraft:book"), Count: 1}

	// The double click on the result itself: a shortcut the take could
	// not serve any better, and nothing it should serve anyway.
	view := grindstoneView(sword, protocol.ItemStack{}, book)
	session := &Session{Carried: held}
	var outcome Outcome
	session.Click(view, &player.Inventory{}, protocol.ClickModeCollect, 0, GrindstoneResultSlot, false, 0, &outcome)
	if got := session.Carried; !got.Equal(held) {
		t.Errorf("carried = %+v, want the cursor untouched by the double click", got)
	}
	if got := view.Menu[GrindstoneResultSlot]; !got.Equal(book) {
		t.Errorf("the result = %+v, want its answer still whole", got)
	}

	// The sweep from a hotbar slot: the cursor holding the result's
	// item gathers the result's stack into it, the inputs emptied.
	inv := &player.Inventory{}
	view = grindstoneView(sword, protocol.ItemStack{}, book)
	session = &Session{Carried: held}
	outcome = Outcome{}
	session.Click(view, inv, protocol.ClickModeCollect, 0, grindstoneHotbarSlot, false, 0, &outcome)
	if got := session.Carried; got.Count != 2 {
		t.Errorf("carried = %+v, want the result's book gathered to the one held", got)
	}
	if !view.Menu[0].Empty() {
		t.Errorf("the first input = %+v, want the gather's take to have emptied it", view.Menu[0])
	}
	if !outcome.GrindstoneTaken || outcome.GrindstoneWorth != 0 {
		t.Errorf("taken %t worth %d, want the gather's take recorded with no magic to pay",
			outcome.GrindstoneTaken, outcome.GrindstoneWorth)
	}
}

// TestTheGrindstonesCollectNeedsTheSameStackAndTheWholeOfIt checks the
// sweep's reach into the result: it gathers only into a cursor holding the
// same item and components, and only when the whole answer fits, a slot
// that cannot be placed into refusing a part-take.
// vanilla: AbstractContainerMenu.canItemQuickReplace, Slot.tryRemove
func TestTheGrindstonesCollectNeedsTheSameStackAndTheWholeOfIt(t *testing.T) {
	sword := grindstoneEnchantedStack(t, "minecraft:diamond_sword", "minecraft:sharpness", 1)
	stripped := protocol.ItemStack{ItemID: sword.ItemID, Count: 1}

	// A cursor of another item never gathers the answer.
	stone := protocol.ItemStack{ItemID: itemID(t, "minecraft:stone"), Count: 5}
	view := grindstoneView(sword, protocol.ItemStack{}, stripped)
	session := &Session{Carried: stone}
	var outcome Outcome
	session.Click(view, &player.Inventory{}, protocol.ClickModeCollect, 0, grindstoneHotbarSlot, false, 0, &outcome)
	if got := session.Carried; !got.Equal(stone) {
		t.Errorf("carried = %+v, want the stone left alone", got)
	}
	if !view.Menu[GrindstoneResultSlot].Equal(stripped) || !view.Menu[0].Equal(sword) {
		t.Errorf("the window = %+v, want it left whole by the mismatched cursor", view.Menu)
	}
	if outcome.GrindstoneTaken {
		t.Error("the take ran for a cursor of another item, want none")
	}

	// The same item with room for less than the whole answer gathers
	// nothing, the result slot's tryRemove refusing a part-take.
	two := protocol.ItemStack{ItemID: itemID(t, "minecraft:book"), Count: 2}
	held := protocol.ItemStack{ItemID: two.ItemID, Count: 63}
	view = grindstoneView(protocol.ItemStack{}, protocol.ItemStack{}, two)
	session = &Session{Carried: held}
	outcome = Outcome{}
	session.Click(view, &player.Inventory{}, protocol.ClickModeCollect, 0, grindstoneHotbarSlot, false, 0, &outcome)
	if got := session.Carried; !got.Equal(held) {
		t.Errorf("carried = %+v, want the book left with a part-take refused", got)
	}
	if !view.Menu[GrindstoneResultSlot].Equal(two) {
		t.Errorf("the result = %+v, want its answer still whole", view.Menu[GrindstoneResultSlot])
	}
	if outcome.GrindstoneTaken {
		t.Error("the take ran without room for the whole answer, want none")
	}

	// The same item with room for all of it gathers the whole answer.
	one := two
	one.Count = 1
	view = grindstoneView(protocol.ItemStack{}, protocol.ItemStack{}, one)
	session = &Session{Carried: held}
	outcome = Outcome{}
	session.Click(view, &player.Inventory{}, protocol.ClickModeCollect, 0, grindstoneHotbarSlot, false, 0, &outcome)
	if got := session.Carried; got.Count != 64 || got.ItemID != one.ItemID {
		t.Errorf("carried = %+v, want the answer gathered to the sixty-three held", got)
	}
	if !outcome.GrindstoneTaken {
		t.Error("the gather's take was not recorded")
	}
}

// TestClosingTheGrindstonesWindowHandsBackItsInputs checks the close: the
// inputs go home to the player, the answer its result was showing goes
// with the window, never handed back as a stack of its own.
func TestClosingTheGrindstonesWindowHandsBackItsInputs(t *testing.T) {
	sword := protocol.ItemStack{ItemID: itemID(t, "minecraft:diamond_sword"), Count: 1}
	pick := protocol.ItemStack{ItemID: itemID(t, "minecraft:diamond_pickaxe"), Count: 1}
	book := protocol.ItemStack{ItemID: itemID(t, "minecraft:book"), Count: 1}

	inv := &player.Inventory{}
	view := grindstoneView(sword, pick, book)
	session := &Session{}
	session.Close(view, inv, false, false)
	if got := inv.Slots[player.HotbarSlot]; !got.Equal(sword) {
		t.Errorf("the hotbar = %+v, want the first input's sword", got)
	}
	if got := inv.Slots[player.HotbarSlot+1]; !got.Equal(pick) {
		t.Errorf("the second hotbar slot = %+v, want the second input's pickaxe", got)
	}
	for _, slot := range inv.Slots {
		if slot.Equal(book) {
			t.Errorf("the inventory holds the result's answer %+v, want it to have gone with the window", slot)
		}
	}
}

// TestQOnTwoVanishingPumpkinsDropsBoth checks the result of count 2 drops
// whole, losing nothing.
func TestQOnTwoVanishingPumpkinsDropsBoth(t *testing.T) {
	pumpkin := grindstoneEnchantedStack(t, "minecraft:carved_pumpkin", "minecraft:vanishing_curse", 1)
	menu := []protocol.ItemStack{pumpkin, pumpkin, {}}
	var outcome Outcome
	RecomputeGrindstone(menu, true)
	view := View{Kind: KindGrindstone, Menu: menu}
	(&Session{}).Click(view, &player.Inventory{}, protocol.ClickModeThrow, 0, GrindstoneResultSlot, false, 0, &outcome)
	var total int32
	for _, d := range outcome.Dropped {
		total += d.Count
	}
	if total != 2 {
		t.Errorf("dropped %v, want 2 items in all", outcome.Dropped)
	}
}
