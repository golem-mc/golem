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
