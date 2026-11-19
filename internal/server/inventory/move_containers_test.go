package inventory

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
)

// TestContainerWindowKindsMatchVanilla checks the shapes of the window kinds
// the containers this server serves open: which menu the Open Screen packet
// names them with, where the player's own things begin within their slot
// numbering, and the title the vanilla server gives them.
func TestContainerWindowKindsMatchVanilla(t *testing.T) {
	for kind, want := range map[Kind]Layout{
		KindBarrel:     {Menu: protocol.MenuGeneric9x3, InvBase: 27, Title: "container.barrel"},
		KindShulker:    {Menu: protocol.MenuShulkerBox, InvBase: 27, Title: "container.shulkerBox"},
		KindEnderChest: {Menu: protocol.MenuGeneric9x3, InvBase: 27, Title: "container.enderchest"},
		KindDispenser:  {Menu: protocol.MenuGeneric3x3, InvBase: 9, Title: "container.dispenser"},
		KindDropper:    {Menu: protocol.MenuGeneric3x3, InvBase: 9, Title: "container.dropper"},
		KindHopper:     {Menu: protocol.MenuHopper, InvBase: 5, Title: "container.hopper"},
	} {
		if got := kind.Layout(); got != want {
			t.Errorf("%d layout = %+v, want %+v", kind, got, want)
		}
	}
	if got := (View{Kind: KindShulker}).Slots(); got != 63 {
		t.Errorf("a shulker box window addresses %d slots, want 63", got)
	}
	if got := (View{Kind: KindHopper}).Slots(); got != 41 {
		t.Errorf("a hopper window addresses %d slots, want 41", got)
	}
}

// TestShulkerRefusesShulkerInShulker checks the one rule the shulker box's
// window adds: a shulker box item may not rest in any of the box's own
// twenty-seven slots. The player's own slots take them as ever.
func TestShulkerRefusesShulkerInShulker(t *testing.T) {
	shulker := protocol.ItemStack{ItemID: itemID(t, "minecraft:shulker_box"), Count: 1}
	view := View{Kind: KindShulker, ID: 1, Entity: make(slots, 27)}
	if view.placeable(0, shulker) {
		t.Error("a shulker box window's own slots take a shulker box, want them refusing it")
	}
	if !view.placeable(view.Kind.Layout().InvBase, shulker) {
		t.Error("a shulker box window's player slots refuse a shulker box, want them taking it")
	}
	if !view.placeable(0, protocol.ItemStack{}) {
		t.Error("a shulker box window's own slots refuse an empty stack, want it in anywhere")
	}
	stone := protocol.ItemStack{ItemID: itemID(t, "minecraft:stone"), Count: 1}
	if !view.placeable(0, stone) {
		t.Error("a shulker box window refuses stone, want it in")
	}

	// The vanilla rule is about the box's own slots, so a barrel takes a
	// shulker box.
	barrel := View{Kind: KindBarrel, ID: 1, Entity: make(slots, 27)}
	if !barrel.placeable(0, shulker) {
		t.Error("a barrel window refuses a shulker box, want it in")
	}

	// And the rule reaches the shift-click: a shulker box shift-clicked
	// from the player's things into a shulker box stays where it was.
	inv := &player.Inventory{}
	inv.Set(player.MainSlot, shulker)
	var outcome Outcome
	session := &Session{}
	session.Click(view, inv, protocol.ClickModeQuickMove, 0, view.Kind.Layout().InvBase,
		false, 0, &outcome)
	if got := inv.Slots[player.MainSlot]; !got.Equal(shulker) {
		t.Errorf("the shulker box = %+v, want it unmoved where shift-clicking it was refused", got)
	}
	if !view.Entity.(slots)[0].Empty() {
		t.Errorf("the box's slot 0 = %+v, want it empty - the refused move went nowhere",
			view.Entity.(slots)[0])
	}
}
