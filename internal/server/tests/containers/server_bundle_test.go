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
