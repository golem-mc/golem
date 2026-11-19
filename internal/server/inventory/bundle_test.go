package inventory

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
)

// bundleOf is a bundle stack holding the given entries, built through the
// same component accessors the click path uses.
func bundleOf(t *testing.T, entries ...protocol.ItemStack) protocol.ItemStack {
	t.Helper()
	stack := protocol.ItemStack{ItemID: itemID(t, "minecraft:bundle"), Count: 1}
	stack.SetBundleContents(entries)
	return stack
}

// TestAFullBundleTradesNothingForAStack checks a full bundle on the cursor
// left-clicked onto a stack leaving both where they were, rather than
// swapping.
func TestAFullBundleTradesNothingForAStack(t *testing.T) {
	clicked := protocol.ItemStack{ItemID: itemID(t, "minecraft:painting"), Count: 1}
	full := bundleOf(t, protocol.ItemStack{ItemID: itemID(t, "minecraft:potion"), Count: 1})

	inv := &player.Inventory{}
	inv.Set(player.MainSlot, clicked)
	session := &Session{Carried: full}
	var outcome Outcome
	session.Click(View{}, inv, protocol.ClickModePickup, 0, player.MainSlot, false, 0, &outcome)

	if got := inv.Slots[player.MainSlot]; !got.Equal(clicked) {
		t.Errorf("slot = %+v, want the clicked stack left in it", got)
	}
	if got := session.Carried; !got.Equal(full) {
		t.Errorf("cursor = %+v, want the full bundle kept", got)
	}
}

// TestABundleTakesTheClickedStack checks the bundle on the cursor being
// filled to whatever it can hold.
func TestABundleTakesTheClickedStack(t *testing.T) {
	stones := protocol.ItemStack{ItemID: itemID(t, "minecraft:stone"), Count: 64}

	inv := &player.Inventory{}
	inv.Set(player.MainSlot, stones)
	session := &Session{Carried: bundleOf(t)}
	var outcome Outcome
	session.Click(View{}, inv, protocol.ClickModePickup, 0, player.MainSlot, false, 0, &outcome)

	if got := inv.Slots[player.MainSlot]; !got.Empty() {
		t.Errorf("slot = %+v, want it emptied by the take", got)
	}
	if got := session.Carried; !got.Equal(bundleOf(t, stones)) {
		t.Errorf("cursor = %+v, want the bundle holding all 64 stone", got)
	}
}

// TestABundleTakesOnlyWhatFits checks an insert that stops at the bundle's
// capacity: the slot keeps the rest.
func TestABundleTakesOnlyWhatFits(t *testing.T) {
	stone := func(count int32) protocol.ItemStack {
		return protocol.ItemStack{ItemID: itemID(t, "minecraft:stone"), Count: count}
	}
	inv := &player.Inventory{}
	inv.Set(player.MainSlot, stone(64))
	session := &Session{Carried: bundleOf(t, stone(32))}
	var outcome Outcome
	session.Click(View{}, inv, protocol.ClickModePickup, 0, player.MainSlot, false, 0, &outcome)

	if got := inv.Slots[player.MainSlot]; !got.Equal(stone(32)) {
		t.Errorf("slot = %+v, want the 32 the bundle could not take", got)
	}
	if got := session.Carried; !got.Equal(bundleOf(t, stone(64))) {
		t.Errorf("cursor = %+v, want the bundle filled to 64 stone", got)
	}
}

// TestABundleWillNotTakeAShulkerBox checks what may not sit inside a
// container staying in its slot when a bundle on the cursor is clicked on it.
func TestABundleWillNotTakeAShulkerBox(t *testing.T) {
	shulker := protocol.ItemStack{ItemID: itemID(t, "minecraft:shulker_box"), Count: 1}
	inv := &player.Inventory{}
	inv.Set(player.MainSlot, shulker)
	session := &Session{Carried: bundleOf(t)}
	var outcome Outcome
	session.Click(View{}, inv, protocol.ClickModePickup, 0, player.MainSlot, false, 0, &outcome)

	if got := inv.Slots[player.MainSlot]; !got.Equal(shulker) {
		t.Errorf("slot = %+v, want the shulker box left in it", got)
	}
	if got := session.Carried; !got.Equal(bundleOf(t)) {
		t.Errorf("cursor = %+v, want the bundle kept", got)
	}
}

// TestARightClickEmptiesABundle checks a right click with a bundle on the
// cursor putting its newest stored stack into an empty slot.
func TestARightClickEmptiesABundle(t *testing.T) {
	stones := protocol.ItemStack{ItemID: itemID(t, "minecraft:stone"), Count: 64}
	inv := &player.Inventory{}
	session := &Session{Carried: bundleOf(t, stones)}
	var outcome Outcome
	session.Click(View{}, inv, protocol.ClickModePickup, 1, player.MainSlot, false, 0, &outcome)

	if got := inv.Slots[player.MainSlot]; !got.Equal(stones) {
		t.Errorf("slot = %+v, want the stored stack put there", got)
	}
	if got := session.Carried; !got.Equal(bundleOf(t)) {
		t.Errorf("cursor = %+v, want an emptied bundle", got)
	}
}

// TestAClickedBundleTakesTheCursorStack checks a bundle in a slot taking the
// stack on the cursor in on a left click.
func TestAClickedBundleTakesTheCursorStack(t *testing.T) {
	stones := protocol.ItemStack{ItemID: itemID(t, "minecraft:stone"), Count: 64}
	inv := &player.Inventory{}
	inv.Set(player.MainSlot, bundleOf(t))
	session := &Session{Carried: stones}
	var outcome Outcome
	session.Click(View{}, inv, protocol.ClickModePickup, 0, player.MainSlot, false, 0, &outcome)

	if got := session.Carried; !got.Empty() {
		t.Errorf("cursor = %+v, want it emptied into the bundle", got)
	}
	if got := inv.Slots[player.MainSlot]; !got.Equal(bundleOf(t, stones)) {
		t.Errorf("slot = %+v, want the bundle holding 64 stone", got)
	}
}

// TestARightClickOnABundleGivesTheCursorAStack checks a bundle in a slot
// giving a stored stack to an empty cursor on a right click.
func TestARightClickOnABundleGivesTheCursorAStack(t *testing.T) {
	stones := protocol.ItemStack{ItemID: itemID(t, "minecraft:stone"), Count: 64}
	inv := &player.Inventory{}
	inv.Set(player.MainSlot, bundleOf(t, stones))
	session := &Session{}
	var outcome Outcome
	session.Click(View{}, inv, protocol.ClickModePickup, 1, player.MainSlot, false, 0, &outcome)

	if got := session.Carried; !got.Equal(stones) {
		t.Errorf("cursor = %+v, want the stored stack on it", got)
	}
	if got := inv.Slots[player.MainSlot]; !got.Equal(bundleOf(t)) {
		t.Errorf("slot = %+v, want an emptied bundle left there", got)
	}
}

// TestSelectBundleClearsOnAnOutOfRangeIndex checks scrolling to an entry the
// bundle does not have selecting nothing.
// vanilla: BundleContents.Mutable.toggleSelectedItem
func TestSelectBundleClearsOnAnOutOfRangeIndex(t *testing.T) {
	session := &Session{selectedBundleSlot: 9, selectedBundleIndex: 0}
	session.SelectBundle(9, 5, 2)
	if session.selectedBundleSlot != -1 || session.selectedBundleIndex != -1 {
		t.Errorf("selection = %d/%d, want it cleared",
			session.selectedBundleSlot, session.selectedBundleIndex)
	}
}

// TestSelectBundleOutOfRangeLeavesAnotherSelection checks an out-of-range
// scroll, or one on a slot holding no bundle, reaching only the named slot: a
// bundle in another slot keeps its selection.
// vanilla: AbstractContainerMenu.setSelectedBundleItemIndex
func TestSelectBundleOutOfRangeLeavesAnotherSelection(t *testing.T) {
	for _, tc := range []struct {
		index   int32
		entries int32
	}{
		{-1, 2},
		{5, 2},
		{0, 0},
	} {
		session := &Session{selectedBundleSlot: 36, selectedBundleIndex: 2}
		session.SelectBundle(37, tc.index, tc.entries)
		if session.selectedBundleSlot != 36 || session.selectedBundleIndex != 2 {
			t.Errorf("SelectBundle(37, %d, %d): selection = %d/%d, want it left at 36/2",
				tc.index, tc.entries, session.selectedBundleSlot, session.selectedBundleIndex)
		}
	}
}

// TestARightClickOnACursorBundleLeavesTheSelection checks a take from a bundle
// on the cursor leaving a slot's scroll position alone: the cursor bundle has
// no window slot, so the selection is another bundle's.
func TestARightClickOnACursorBundleLeavesTheSelection(t *testing.T) {
	stones := protocol.ItemStack{ItemID: itemID(t, "minecraft:stone"), Count: 64}
	inv := &player.Inventory{}
	session := &Session{Carried: bundleOf(t, stones)}
	session.selectedBundleSlot = player.MainSlot
	session.selectedBundleIndex = 0
	var outcome Outcome
	session.Click(View{}, inv, protocol.ClickModePickup, 1, player.MainSlot, false, 0, &outcome)

	if session.selectedBundleSlot != player.MainSlot || session.selectedBundleIndex != 0 {
		t.Errorf("selection = %d/%d, want it left at %d/0",
			session.selectedBundleSlot, session.selectedBundleIndex, player.MainSlot)
	}
}

// TestABundleTakesOneOccupiedBeehive checks a beehive with bees inside
// filling a bundle on its own, and a bundle holding one taking nothing else.
func TestABundleTakesOneOccupiedBeehive(t *testing.T) {
	hive := protocol.ItemStack{ItemID: itemID(t, "minecraft:beehive"), Count: 2}
	if !hive.SetBees([]protocol.Bee{{EntityType: 0}}) {
		t.Fatal("SetBees failed")
	}
	inv := &player.Inventory{}
	inv.Set(player.MainSlot, hive)
	session := &Session{Carried: bundleOf(t)}
	var outcome Outcome
	session.Click(View{}, inv, protocol.ClickModePickup, 0, player.MainSlot, false, 0, &outcome)

	one, rest := hive, hive
	one.Count, rest.Count = 1, 1
	if got := inv.Slots[player.MainSlot]; !got.Equal(rest) {
		t.Errorf("slot = %+v, want one hive left", got)
	}
	if got := session.Carried; !got.Equal(bundleOf(t, one)) {
		t.Errorf("cursor = %+v, want a bundle holding one hive", got)
	}

	stick := protocol.ItemStack{ItemID: itemID(t, "minecraft:stick"), Count: 1}
	inv.Set(player.MainSlot, stick)
	session.Click(View{}, inv, protocol.ClickModePickup, 0, player.MainSlot, false, 0, &outcome)
	if got := inv.Slots[player.MainSlot]; !got.Equal(stick) {
		t.Errorf("slot = %+v, want the stick left", got)
	}
	if got := session.Carried; !got.Equal(bundleOf(t, one)) {
		t.Errorf("cursor = %+v, want the bundle unchanged", got)
	}
}
