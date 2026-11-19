package containers

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
)

// Bundle clicks: a bundle fills and empties instead of trading places, the
// overrides BundleItem.overrideStackedOnOther and overrideOtherStackedOnMe
// taking the click over. The bundle_contents value is written here by hand, in
// the shape a client sends, so the tests speak the wire the server reads.

// rawBundle is a bundle stack holding the given entries, each written as the
// item, the count and an empty component patch inside the
// minecraft:bundle_contents component. A bundle with no entries carries no
// component, the item's own default.
func rawBundle(t *testing.T, entries ...protocol.ItemStack) protocol.ItemStack {
	t.Helper()
	stack := protocol.ItemStack{ItemID: servertest.ItemID(t, "minecraft:bundle"), Count: 1}
	if len(entries) == 0 {
		return stack
	}
	var value protocol.Buffer
	value.PutVarInt(int32(len(entries)))
	for _, entry := range entries {
		value.PutVarInt(entry.ItemID)
		value.PutVarInt(entry.Count)
		value.PutVarInt(0) // no components added
		value.PutVarInt(0) // none taken back to defaults
	}
	stack.Raw = []protocol.RawComponent{{
		Type: protocol.DataComponentBundleContents,
		Data: value.Bytes(),
	}}
	return stack
}

// TestABundleOnTheCursorDoesNotTradeForAFullOne checks a full bundle (one
// potion) on the cursor left-clicked onto a painting: the insert fails and
// both stay where they were rather than trading places.
func TestABundleOnTheCursorDoesNotTradeForAFullOne(t *testing.T) {
	t.Parallel()
	_, w := servertest.OpenWindowed(t, servertest.BlockTestConfig())

	full := rawBundle(t, servertest.ItemStack(t, "minecraft:potion", 1))
	painting := servertest.ItemStack(t, "minecraft:painting", 1)
	w.SetCreativeSlot(36, full)
	w.SetCreativeSlot(37, painting)

	w.Click(36, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{36: {}}, full)

	w.Click(37, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{}, full)
}

// TestABundleOnTheCursorFillsFromTheClickedStack checks a bundle on the
// cursor left-clicked onto an insertable stack taking it in rather than
// swapping.
func TestABundleOnTheCursorFillsFromTheClickedStack(t *testing.T) {
	t.Parallel()
	_, w := servertest.OpenWindowed(t, servertest.BlockTestConfig())

	empty := rawBundle(t)
	stones := servertest.ItemStack(t, "minecraft:stone", 64)
	w.SetCreativeSlot(36, empty)
	w.SetCreativeSlot(37, stones)

	w.Click(36, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{36: {}}, empty)

	w.Click(37, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{37: {}}, rawBundle(t, stones))
}

// TestARightClickEmptiesABundleIntoAnEmptySlot checks the bundle on the
// cursor giving a stored stack back when a right click lands on an empty
// slot.
func TestARightClickEmptiesABundleIntoAnEmptySlot(t *testing.T) {
	t.Parallel()
	_, w := servertest.OpenWindowed(t, servertest.BlockTestConfig())

	stones := servertest.ItemStack(t, "minecraft:stone", 64)
	loaded := rawBundle(t, stones)
	w.SetCreativeSlot(36, loaded)

	w.Click(36, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{36: {}}, loaded)

	w.Click(37, 1, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{37: stones}, rawBundle(t))
}

// TestAClickedBundleTakesTheCursorStack checks a bundle already in a slot
// taking the stack on the cursor, the other override.
func TestAClickedBundleTakesTheCursorStack(t *testing.T) {
	t.Parallel()
	_, w := servertest.OpenWindowed(t, servertest.BlockTestConfig())

	stones := servertest.ItemStack(t, "minecraft:stone", 64)
	w.SetCreativeSlot(36, stones)
	w.SetCreativeSlot(37, rawBundle(t))

	w.Click(36, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{36: {}}, stones)

	w.Click(37, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{37: rawBundle(t, stones)}, protocol.ItemStack{})
}

// TestARightClickOnABundleGivesItsStackToTheCursor checks a right click on a
// bundle in a slot, with an empty cursor, taking a stored stack out.
func TestARightClickOnABundleGivesItsStackToTheCursor(t *testing.T) {
	t.Parallel()
	_, w := servertest.OpenWindowed(t, servertest.BlockTestConfig())

	stones := servertest.ItemStack(t, "minecraft:stone", 64)
	w.SetCreativeSlot(36, rawBundle(t, stones))

	w.Click(36, 1, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{36: rawBundle(t)}, stones)
}

// TestARightClickTakesTheSelectedBundleItem checks the Bundle Item Selected
// packet: after the client scrolls a slot's bundle to an entry, a right click
// takes that entry rather than the newest.
func TestARightClickTakesTheSelectedBundleItem(t *testing.T) {
	t.Parallel()
	_, w := servertest.OpenWindowed(t, servertest.BlockTestConfig())

	stones := servertest.ItemStack(t, "minecraft:stone", 64)
	dirt := servertest.ItemStack(t, "minecraft:dirt", 64)
	w.SetCreativeSlot(36, rawBundle(t, stones, dirt))

	w.SelectBundle(36, 1)
	w.Click(36, 1, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{36: rawBundle(t, stones)}, dirt)
}

// TestARightClickClearsTheSelectedBundleItem checks the selection not
// outliving the take it named: once a right click has removed the scrolled-to
// entry, the next takes the newest, not the entry that followed it.
// vanilla: BundleContents.Mutable.removeOne
func TestARightClickClearsTheSelectedBundleItem(t *testing.T) {
	t.Parallel()
	_, w := servertest.OpenWindowed(t, servertest.BlockTestConfig())

	stones := servertest.ItemStack(t, "minecraft:stone", 64)
	dirt := servertest.ItemStack(t, "minecraft:dirt", 64)
	sand := servertest.ItemStack(t, "minecraft:sand", 64)
	gravel := servertest.ItemStack(t, "minecraft:gravel", 64)
	w.SetCreativeSlot(36, rawBundle(t, stones, dirt, sand, gravel))

	w.SelectBundle(36, 2)
	w.Click(36, 1, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{36: rawBundle(t, stones, dirt, gravel)}, sand)

	// Put the sand down so the next right click starts on an empty cursor.
	w.Click(37, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{37: sand}, protocol.ItemStack{})

	w.Click(36, 1, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{36: rawBundle(t, dirt, gravel)}, stones)
}

// TestASelectedBundleItemTogglesOff checks the same index sent twice clearing
// the selection, so the click takes the newest again.
// vanilla: BundleContents.Mutable.toggleSelectedItem
func TestASelectedBundleItemTogglesOff(t *testing.T) {
	t.Parallel()
	_, w := servertest.OpenWindowed(t, servertest.BlockTestConfig())

	stones := servertest.ItemStack(t, "minecraft:stone", 64)
	dirt := servertest.ItemStack(t, "minecraft:dirt", 64)
	w.SetCreativeSlot(36, rawBundle(t, stones, dirt))

	w.SelectBundle(36, 1)
	w.SelectBundle(36, 1)
	w.Click(36, 1, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{36: rawBundle(t, dirt)}, stones)
}

// TestAClickOnOneBundleLeavesAnothersSelection checks the selection being per
// bundle: a click that clears a scroll position only clears the clicked
// bundle's own, so another bundle still takes the entry the player selected.
func TestAClickOnOneBundleLeavesAnothersSelection(t *testing.T) {
	t.Parallel()
	_, w := servertest.OpenWindowed(t, servertest.BlockTestConfig())

	stones := servertest.ItemStack(t, "minecraft:stone", 64)
	dirt := servertest.ItemStack(t, "minecraft:dirt", 64)
	sand := servertest.ItemStack(t, "minecraft:sand", 64)
	gravel := servertest.ItemStack(t, "minecraft:gravel", 64)
	w.SetCreativeSlot(36, rawBundle(t, stones, dirt, sand))
	w.SetCreativeSlot(37, rawBundle(t, gravel))

	w.SelectBundle(36, 2)

	// Pick bundle B up and put it back: two clicks on the other bundle, which
	// must leave A's selection alone.
	w.Click(37, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{37: {}}, rawBundle(t, gravel))
	w.Click(37, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{37: rawBundle(t, gravel)}, protocol.ItemStack{})

	w.Click(36, 1, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{36: rawBundle(t, stones, dirt)}, sand)
}

// TestAnOutOfRangeScrollLeavesAnothersSelection checks a scroll past another
// bundle's entries reaching only that slot: the first bundle's selection
// stands, so its click still takes the scrolled-to entry rather than the
// newest.
// vanilla: AbstractContainerMenu.setSelectedBundleItemIndex
func TestAnOutOfRangeScrollLeavesAnothersSelection(t *testing.T) {
	t.Parallel()
	_, w := servertest.OpenWindowed(t, servertest.BlockTestConfig())

	stones := servertest.ItemStack(t, "minecraft:stone", 64)
	dirt := servertest.ItemStack(t, "minecraft:dirt", 64)
	sand := servertest.ItemStack(t, "minecraft:sand", 64)
	gravel := servertest.ItemStack(t, "minecraft:gravel", 64)
	w.SetCreativeSlot(36, rawBundle(t, stones, dirt, sand))
	w.SetCreativeSlot(37, rawBundle(t, gravel))

	w.SelectBundle(36, 2)
	w.SelectBundle(37, 5)

	w.Click(36, 1, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{36: rawBundle(t, stones, dirt)}, sand)
}

// TestANegativeBundleSlotLeavesTheSelection checks a packet naming no window
// slot not moving the selection: vanilla ignores it.
// vanilla: AbstractContainerMenu.setSelectedBundleItemIndex
func TestANegativeBundleSlotLeavesTheSelection(t *testing.T) {
	t.Parallel()
	_, w := servertest.OpenWindowed(t, servertest.BlockTestConfig())

	stones := servertest.ItemStack(t, "minecraft:stone", 64)
	dirt := servertest.ItemStack(t, "minecraft:dirt", 64)
	w.SetCreativeSlot(36, rawBundle(t, stones, dirt))

	w.SelectBundle(36, 1)
	w.SelectBundle(-1, 0)
	w.Click(36, 1, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{36: rawBundle(t, stones)}, dirt)
}
