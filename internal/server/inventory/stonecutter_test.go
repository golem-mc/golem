package inventory

import (
	"slices"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
)

// The stonecutter window over the given input and the result slot's
// stored truth, laid out the way a pick of its buttons leaves them.
func stonecutterView(input, result protocol.ItemStack) View {
	return View{Kind: KindStonecutter, Menu: []protocol.ItemStack{input, result}}
}

// stonecutterHotbarSlot is where the hotbar sits in the stonecutter's
// window: the input and the result ahead of the main twenty-seven.
const stonecutterHotbarSlot = StonecutterResultSlot + 1 + (player.HotbarSlot - player.MainSlot)

// TestTheStonecuttersRecipesNumberInTheOrderSent checks the numbering the
// buttons pick by: the index counts the recipes the join's Update
// Recipes list offers the input item, one past its list is no recipe, and
// negative none either.
func TestTheStonecuttersRecipesNumberInTheOrderSent(t *testing.T) {
	stone := itemID(t, "minecraft:stone")

	// Stone's list, the generated table's own order: chiseled bricks
	// first, the two-cobblestone-slab cut third.
	if got, ok := StonecutterResult(stone, 0); !got.Equal(protocol.ItemStack{
		ItemID: itemID(t, "minecraft:chiseled_stone_bricks"), Count: 1,
	}) || !ok {
		t.Errorf("stone's recipe 0 = %+v, %t, want its chiseled bricks", got, ok)
	}
	if got, ok := StonecutterResult(stone, 2); !got.Equal(protocol.ItemStack{
		ItemID: itemID(t, "minecraft:cobblestone_slab"), Count: 2,
	}) || !ok {
		t.Errorf("stone's recipe 2 = %+v, %t, want the two slabs", got, ok)
	}

	// An index past the list and negative are nothing; so is an input
	// the stonecutter does not cut.
	for _, miss := range []int32{11, -1} {
		if got, ok := StonecutterResult(stone, miss); ok || !got.Empty() {
			t.Errorf("stone's recipe %d = %+v, %t, want nothing", miss, got, ok)
		}
	}
	if got, ok := StonecutterResult(0, 0); ok || !got.Empty() {
		t.Errorf("an empty cut = %+v, %t, want nothing", got, ok)
	}
}

// TestTheStonecutterSpendsOneInputPerTake checks the take: whatever a
// recipe's output is worth, one input goes, the result keeps offering the
// same cut, and the input's last piece takes the result with it.
func TestTheStonecutterSpendsOneInputPerTake(t *testing.T) {
	stone := protocol.ItemStack{ItemID: itemID(t, "minecraft:stone"), Count: 2}
	bricks := protocol.ItemStack{ItemID: itemID(t, "minecraft:stone_bricks"), Count: 1}
	stoneTwo := stone
	stoneTwo.Count = 1

	// The first take of a two-stone input: one goes, and the result
	// still offers the cut, unchecked by the take.
	view := stonecutterView(stone, bricks)
	session := &Session{}
	var outcome Outcome
	session.Click(view, &player.Inventory{}, protocol.ClickModePickup, 0, StonecutterResultSlot, false, 0, &outcome)
	if got := session.Carried; !got.Equal(bricks) {
		t.Errorf("carried = %+v, want the result's stack", got)
	}
	if got := view.Menu[0]; !got.Equal(stoneTwo) {
		t.Errorf("the input = %+v, want one stone of two", got)
	}
	if got := view.Menu[StonecutterResultSlot]; !got.Equal(bricks) {
		t.Errorf("the result = %+v, want the cut still on offer", got)
	}
	if !outcome.StonecutterTaken {
		t.Error("the take was not marked, want its sound paid for")
	}

	// The second take spends the last stone under the same session's
	// cursor, and the result slot goes empty with the input.
	outcome = Outcome{}
	session.Click(view, &player.Inventory{}, protocol.ClickModePickup, 0, StonecutterResultSlot, false, 0, &outcome)
	if got := session.Carried; got.Count != 2 {
		t.Errorf("carried = %+v, want both cuts' worth", got)
	}
	if !view.Menu[0].Empty() {
		t.Errorf("the input = %+v, want it spent", view.Menu[0])
	}
	if !view.Menu[StonecutterResultSlot].Empty() {
		t.Errorf("the result = %+v, want nothing left to cut", view.Menu[StonecutterResultSlot])
	}
	if !slices.Contains(outcome.Announce, 0) || !slices.Contains(outcome.Announce, 1) {
		t.Errorf("announced %v, want the input and the emptied result", outcome.Announce)
	}

	// With nothing to cut, the next take announces and spends nothing.
	outcome = Outcome{}
	session.Click(view, &player.Inventory{}, protocol.ClickModePickup, 0, StonecutterResultSlot, false, 0, &outcome)
	if got := session.Carried; got.Count != 2 {
		t.Errorf("carried = %+v, want the take to have given nothing", got)
	}
	if len(outcome.Announce) != 0 || outcome.StonecutterTaken {
		t.Errorf("announced %v (taken %t), want a dead window", outcome.Announce, outcome.StonecutterTaken)
	}
}

// TestAPlainClickOnTheStonecuttersResultTakesItWhole checks the plain
// take's button: the result container's removeItem ignores the amount a
// right click asks for, so both buttons hand the whole cut over, the one
// stone behind it spent.
// vanilla: AbstractContainerMenu.doClick PICKUP, ResultContainer.removeItem
func TestAPlainClickOnTheStonecuttersResultTakesItWhole(t *testing.T) {
	stone := protocol.ItemStack{ItemID: itemID(t, "minecraft:stone"), Count: 1}
	slabs := protocol.ItemStack{ItemID: itemID(t, "minecraft:cobblestone_slab"), Count: 2}

	for _, button := range []int32{0, 1} {
		view := stonecutterView(stone, slabs)
		session := &Session{}
		var outcome Outcome
		session.Click(view, &player.Inventory{}, protocol.ClickModePickup, button, StonecutterResultSlot, false, 0, &outcome)
		if got := session.Carried; !got.Equal(slabs) {
			t.Errorf("button %d carried = %+v, want both slabs", button, got)
		}
		if !view.Menu[0].Empty() {
			t.Errorf("button %d input = %+v, want the stone spent", button, view.Menu[0])
		}
		if !view.Menu[StonecutterResultSlot].Empty() {
			t.Errorf("button %d result = %+v, want nothing left to cut", button, view.Menu[StonecutterResultSlot])
		}
		if !outcome.StonecutterTaken {
			t.Errorf("button %d was not marked, want its sound paid for", button)
		}
	}
}

// TestTheStonecuttersResultGivesNothingToAnotherItemOnTheCursor checks the
// take under a cursor holding something the result is not: no take at
// all, the input where it was.
func TestTheStonecuttersResultGivesNothingToAnotherItemOnTheCursor(t *testing.T) {
	stone := protocol.ItemStack{ItemID: itemID(t, "minecraft:stone"), Count: 2}
	bricks := protocol.ItemStack{ItemID: itemID(t, "minecraft:stone_bricks"), Count: 1}
	dirt := protocol.ItemStack{ItemID: itemID(t, "minecraft:dirt"), Count: 1}

	view := stonecutterView(stone, bricks)
	session := &Session{Carried: dirt}
	var outcome Outcome
	session.Click(view, &player.Inventory{}, protocol.ClickModePickup, 0, StonecutterResultSlot, false, 0, &outcome)
	if got := session.Carried; !got.Equal(dirt) {
		t.Errorf("carried = %+v, want the cursor untouched", got)
	}
	if !view.Menu[0].Equal(stone) {
		t.Errorf("the input = %+v, want both its stones", view.Menu[0])
	}
	if got := view.Menu[StonecutterResultSlot]; !got.Equal(bricks) {
		t.Errorf("the result = %+v, want the cut still on offer", got)
	}
	if len(outcome.Announce) != 0 || outcome.StonecutterTaken {
		t.Errorf("announced %v (taken %t), want a refused take", outcome.Announce, outcome.StonecutterTaken)
	}
}

// TestASameItemCursorWithTooLittleRoomTakesNothingFromTheStonecutter
// checks the same-item take's refusal: a cursor holding the result's item
// but short of room for the whole offer takes none of it, and the input is
// left unspent, where a part of the offer that fit would have gone.
func TestASameItemCursorWithTooLittleRoomTakesNothingFromTheStonecutter(t *testing.T) {
	slabs := protocol.ItemStack{ItemID: itemID(t, "minecraft:cobblestone_slab"), Count: 2}
	stone := protocol.ItemStack{ItemID: itemID(t, "minecraft:stone"), Count: 2}
	held := protocol.ItemStack{ItemID: itemID(t, "minecraft:cobblestone_slab"), Count: 63}

	// One room short of the two-slab offer: no take at all.
	view := stonecutterView(stone, slabs)
	session := &Session{Carried: held}
	var outcome Outcome
	session.Click(view, &player.Inventory{}, protocol.ClickModePickup, 0, StonecutterResultSlot, false, 0, &outcome)
	if got := session.Carried; !got.Equal(held) {
		t.Errorf("carried = %+v, want the cursor untouched with one room short", got)
	}
	if !view.Menu[0].Equal(stone) {
		t.Errorf("the input = %+v, want both its stones unspent", view.Menu[0])
	}
	if got := view.Menu[StonecutterResultSlot]; !got.Equal(slabs) {
		t.Errorf("the result = %+v, want the offer still whole", got)
	}
	if outcome.StonecutterTaken {
		t.Error("a refused take was marked taken, want no sound")
	}

	// The room for the whole offer is what the take needs.
	roomy := held
	roomy.Count = 62
	session = &Session{Carried: roomy}
	outcome = Outcome{}
	session.Click(view, &player.Inventory{}, protocol.ClickModePickup, 0, StonecutterResultSlot, false, 0, &outcome)
	if got := session.Carried.Count; got != 64 {
		t.Errorf("carried = %d, want the whole offer onto the cursor", got)
	}
	if !outcome.StonecutterTaken {
		t.Error("the take was not marked, want its sound paid for")
	}
}

// TestADoubleClickOnTheEmptyStonecutterResultGathers checks the collect:
// with the result empty and the cursor holding its item, a double click on
// the empty result walks the other slots - the input among them - since
// only the result container is out of the sweep.
func TestADoubleClickOnTheEmptyStonecutterResultGathers(t *testing.T) {
	stone := protocol.ItemStack{ItemID: itemID(t, "minecraft:stone"), Count: 2}
	held := protocol.ItemStack{ItemID: itemID(t, "minecraft:stone"), Count: 1}

	view := stonecutterView(stone, protocol.ItemStack{})
	session := &Session{Carried: held}
	var outcome Outcome
	session.Click(view, &player.Inventory{}, protocol.ClickModeCollect, 0, StonecutterResultSlot, false, 0, &outcome)
	if got := session.Carried.Count; got != 3 {
		t.Errorf("carried = %+v, want the input's two stones gathered", got)
	}
	if !view.Menu[0].Empty() {
		t.Errorf("the input = %+v, want it swept into the cursor", view.Menu[0])
	}
}

// TestADoubleClickElsewhereNeverGathersTheStonecutterResult checks the
// sweep's exclusion: a double click on another slot, the cursor holding
// the result's item, leaves the result container alone.
// vanilla: StonecutterMenu.canTakeItemForPickAll
func TestADoubleClickElsewhereNeverGathersTheStonecutterResult(t *testing.T) {
	stone := protocol.ItemStack{ItemID: itemID(t, "minecraft:stone"), Count: 2}
	bricks := protocol.ItemStack{ItemID: itemID(t, "minecraft:stone_bricks"), Count: 1}

	inv := &player.Inventory{}
	view := stonecutterView(stone, bricks)
	session := &Session{Carried: bricks}
	var outcome Outcome
	session.Click(view, inv, protocol.ClickModeCollect, 0, stonecutterHotbarSlot, false, 0, &outcome)
	if got := session.Carried; !got.Equal(bricks) {
		t.Errorf("carried = %+v, want the brick left behind", got)
	}
	if got := view.Menu[StonecutterResultSlot]; !got.Equal(bricks) {
		t.Errorf("the result = %+v, want the cut still on offer", got)
	}
	if got := view.Menu[0]; !got.Equal(stone) {
		t.Errorf("the input = %+v, want both its stones", got)
	}
}

// TestAShiftClickFeedsTheStonecuttersInput checks the shift-click's
// routing: what the stonecutter cuts lays in the input whole, anything
// else moves among the player's own rows, and the input home to them.
func TestAShiftClickFeedsTheStonecuttersInput(t *testing.T) {
	stone := protocol.ItemStack{ItemID: itemID(t, "minecraft:stone"), Count: 5}
	dirt := protocol.ItemStack{ItemID: itemID(t, "minecraft:dirt"), Count: 5}

	// A stonecut stack lays in the input whole, the vanilla input slot
	// keeping an item's own stack size.
	inv := &player.Inventory{}
	inv.Set(player.HotbarSlot, stone)
	view := stonecutterView(protocol.ItemStack{}, protocol.ItemStack{})
	var outcome Outcome
	session := &Session{}
	session.Click(view, inv, protocol.ClickModeQuickMove, 0, stonecutterHotbarSlot, false, 0, &outcome)
	if got := view.Menu[0]; !got.Equal(stone) {
		t.Errorf("the input = %+v, want the whole stack of stone", got)
	}
	if got := inv.Slots[player.HotbarSlot]; !got.Empty() {
		t.Errorf("the hotbar = %+v, want it emptied by the move", got)
	}

	// What nothing cuts moves among the player's things: the hotbar's
	// stack into the main rows.
	inv = &player.Inventory{}
	inv.Set(player.HotbarSlot, dirt)
	view = stonecutterView(protocol.ItemStack{}, protocol.ItemStack{})
	outcome = Outcome{}
	session.Click(view, inv, protocol.ClickModeQuickMove, 0, stonecutterHotbarSlot, false, 0, &outcome)
	if got := view.Menu[0]; !got.Empty() {
		t.Errorf("the input = %+v, want the dirt left out of it", got)
	}
	if got := inv.Slots[player.MainSlot]; !got.Equal(dirt) {
		t.Errorf("the main rows = %+v, want the dirt among them", got)
	}

	// The input itself shifts home to the player's things, the main
	// rows first like the anvil's inputs.
	inv = &player.Inventory{}
	view = stonecutterView(stone, protocol.ItemStack{})
	outcome = Outcome{}
	session.Click(view, inv, protocol.ClickModeQuickMove, 0, 0, false, 0, &outcome)
	if got := inv.Slots[player.MainSlot]; !got.Equal(stone) {
		t.Errorf("the main rows = %+v, want the input's stone", got)
	}
	if !view.Menu[0].Empty() {
		t.Errorf("the input = %+v, want it shifted home", view.Menu[0])
	}
}

// TestANumberKeyTakesTheStonecuttersResultWhole checks the swap, a number
// key over the result: the whole of it into an empty hotbar slot, its take
// spending the one input - and into a taken slot none of it, the result
// giving and never receiving.
func TestANumberKeyTakesTheStonecuttersResultWhole(t *testing.T) {
	stone := protocol.ItemStack{ItemID: itemID(t, "minecraft:stone"), Count: 2}
	bricks := protocol.ItemStack{ItemID: itemID(t, "minecraft:stone_bricks"), Count: 1}
	stoneOne := stone
	stoneOne.Count = 1

	// The first hotbar slot takes the whole result.
	inv := &player.Inventory{}
	view := stonecutterView(stone, bricks)
	session := &Session{}
	var outcome Outcome
	session.Click(view, inv, protocol.ClickModeSwap, 0, StonecutterResultSlot, false, 0, &outcome)
	if got := inv.Slots[player.HotbarSlot]; !got.Equal(bricks) {
		t.Errorf("the hotbar = %+v, want the result's whole stack", got)
	}
	if got := view.Menu[0]; !got.Equal(stoneOne) {
		t.Errorf("the input = %+v, want the swap's take to spend a stone", got)
	}
	if !outcome.StonecutterTaken {
		t.Error("the swap's take was not marked, want its sound paid for")
	}

	// A hotbar slot already holding something takes nothing back, the
	// result being no slot to swap into.
	dirt := protocol.ItemStack{ItemID: itemID(t, "minecraft:dirt"), Count: 1}
	inv = &player.Inventory{}
	inv.Set(player.HotbarSlot, dirt)
	view = stonecutterView(stone, bricks)
	outcome = Outcome{}
	session.Click(view, inv, protocol.ClickModeSwap, 0, StonecutterResultSlot, false, 0, &outcome)
	if got := inv.Slots[player.HotbarSlot]; !got.Equal(dirt) {
		t.Errorf("the hotbar = %+v, want the dirt left in it", got)
	}
	if got := view.Menu[StonecutterResultSlot]; !got.Equal(bricks) {
		t.Errorf("the result = %+v, want the cut still on offer", got)
	}
}

// TestTheDropKeyThrowsTheStonecuttersResultOut checks the Q's take: the
// whole offer for a plain Q - the result container's removeItem ignores
// the amount a plain Q asks for - and, for Ctrl+Q, the whole offer once
// per piece of input, the input spent behind every throw.
func TestTheDropKeyThrowsTheStonecuttersResultOut(t *testing.T) {
	stone := protocol.ItemStack{ItemID: itemID(t, "minecraft:stone"), Count: 2}
	bricks := protocol.ItemStack{ItemID: itemID(t, "minecraft:stone_bricks"), Count: 3}
	stoneOne := stone
	stoneOne.Count = 1

	// The plain Q drops the whole offer the result holds.
	view := stonecutterView(stone, bricks)
	session := &Session{}
	var outcome Outcome
	session.Click(view, &player.Inventory{}, protocol.ClickModeThrow, 0, StonecutterResultSlot, false, 0, &outcome)
	if len(outcome.Dropped) != 1 || !outcome.Dropped[0].Equal(bricks) {
		t.Errorf("dropped %v, want the plain Q's whole offer", outcome.Dropped)
	}
	if got := view.Menu[0]; !got.Equal(stoneOne) {
		t.Errorf("the input = %+v, want the throw's take to spend a stone", got)
	}

	// Ctrl+Q throws the whole offer once per stone until the input runs
	// out: two stones behind two whole-offer drops, the window emptied.
	view = stonecutterView(stone, bricks)
	outcome = Outcome{}
	session.Click(view, &player.Inventory{}, protocol.ClickModeThrow, 1, StonecutterResultSlot, false, 0, &outcome)
	if len(outcome.Dropped) != 2 || !outcome.Dropped[0].Equal(bricks) || !outcome.Dropped[1].Equal(bricks) {
		t.Errorf("dropped %v, want two whole-offer throws", outcome.Dropped)
	}
	if !view.Menu[0].Empty() {
		t.Errorf("the input = %+v, want it spent by the throws", view.Menu[0])
	}
	if !view.Menu[StonecutterResultSlot].Empty() {
		t.Errorf("the result = %+v, want nothing left to throw", view.Menu[StonecutterResultSlot])
	}

	// A cut worth more than one throws its whole count per take.
	slabs := protocol.ItemStack{ItemID: itemID(t, "minecraft:cobblestone_slab"), Count: 2}
	three := protocol.ItemStack{ItemID: itemID(t, "minecraft:stone"), Count: 3}
	view = stonecutterView(three, slabs)
	outcome = Outcome{}
	session.Click(view, &player.Inventory{}, protocol.ClickModeThrow, 1, StonecutterResultSlot, false, 0, &outcome)
	if len(outcome.Dropped) != 3 {
		t.Fatalf("dropped %v, want three takes", outcome.Dropped)
	}
	for i, drop := range outcome.Dropped {
		if !drop.Equal(slabs) {
			t.Errorf("drop %d = %+v, want the whole two-slab offer", i, drop)
		}
	}
	if !view.Menu[0].Empty() || !view.Menu[StonecutterResultSlot].Empty() {
		t.Errorf("the window = %+v, want it empty", view.Menu)
	}
}

// TestClosingTheStonecuttersWindowHandsBackTheInput checks the close: the
// input goes home to the player, the cut its result was offering goes with
// the window, never handed back as a stack of its own.
func TestClosingTheStonecuttersWindowHandsBackTheInput(t *testing.T) {
	stone := protocol.ItemStack{ItemID: itemID(t, "minecraft:stone"), Count: 2}
	bricks := protocol.ItemStack{ItemID: itemID(t, "minecraft:stone_bricks"), Count: 1}

	inv := &player.Inventory{}
	view := stonecutterView(stone, bricks)
	session := &Session{}
	back := session.Close(view, inv, false, false)
	if got := inv.Slots[player.HotbarSlot]; !got.Equal(stone) {
		t.Errorf("the hotbar = %+v, want the input's stones", got)
	}
	for _, slot := range inv.Slots {
		if slot.Equal(bricks) {
			t.Errorf("the inventory holds the result's bricks %+v, want the cut to have gone with the window", slot)
		}
	}
	if back.Changed() && !slices.Contains(back.Slots, player.HotbarSlot) {
		t.Errorf("the handback's slots = %v, want the hotbar in them", back.Slots)
	}
}
