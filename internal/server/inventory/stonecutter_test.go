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
