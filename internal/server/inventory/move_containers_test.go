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

// TestContainerWindowsResolveTheirOwnSlotsAsEntitySlots checks that every
// one of the container windows addresses the slots ahead of the
// player's things as its block entity's, what makes the clicks land in
// the container the window was opened on.
func TestContainerWindowsResolveTheirOwnSlotsAsEntitySlots(t *testing.T) {
	for _, kind := range []Kind{KindBarrel, KindShulker, KindEnderChest, KindDispenser, KindDropper, KindHopper} {
		for slot := int32(0); slot < kind.Layout().InvBase; slot++ {
			if loc, _ := (View{Kind: kind}).resolve(slot); loc != slotEntity {
				t.Errorf("%d slot %d resolves to %d, want an entity slot", kind, slot, loc)
			}
		}
		if loc, _ := (View{Kind: kind}).resolve(kind.Layout().InvBase); loc != slotInv {
			t.Errorf("%d slot %d resolves to %d, want a player slot", kind, kind.Layout().InvBase, loc)
		}
	}
}

// TestShiftClickRoutesThroughTheNewContainers checks the shift-click
// through each of these windows: out of the container's own slots into
// the player's things, and back in, the way the chest menus route.
func TestShiftClickRoutesThroughTheNewContainers(t *testing.T) {
	apples := protocol.ItemStack{ItemID: itemID(t, "minecraft:apple"), Count: 10}

	for _, kind := range []Kind{KindBarrel, KindShulker, KindEnderChest, KindDispenser, KindDropper, KindHopper} {
		own := kind.Layout().InvBase
		container := make(slots, own)
		container[1] = apples
		inv := &player.Inventory{}
		var outcome Outcome
		session := &Session{}
		session.Click(View{Kind: kind, ID: 1, Entity: container}, inv,
			protocol.ClickModeQuickMove, 0, 1, false, 0, &outcome)
		if got := inv.Slots[player.HotbarSlot+8]; !got.Equal(apples) {
			t.Errorf("%d: the last hotbar slot = %+v, want the apples from the container", kind, got)
		}
		if !container[1].Empty() {
			t.Errorf("%d: container slot 1 = %+v, want it emptied by the move", kind, container[1])
		}

		// And back in, into the container's first free slot.
		container = make(slots, own)
		inv = &player.Inventory{}
		inv.Set(player.MainSlot, apples)
		outcome = Outcome{}
		session = &Session{}
		session.Click(View{Kind: kind, ID: 1, Entity: container}, inv,
			protocol.ClickModeQuickMove, 0, int32(own), false, 0, &outcome)
		if !container[0].Equal(apples) {
			t.Errorf("%d: container slot 0 = %+v, want the apples moved in", kind, container[0])
		}
		if !inv.Slots[player.MainSlot].Empty() {
			t.Errorf("%d: main inventory slot 9 = %+v, want it emptied by the move", kind, inv.Slots[player.MainSlot])
		}
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

// TestShulkerColourRefusesShulkerToo checks that the rule is about the item
// being a shulker box, whatever colour it wears: the sixteen coloured ones
// are as little able to go in as the undyed one.
func TestShulkerColourRefusesShulkerToo(t *testing.T) {
	view := View{Kind: KindShulker, ID: 1, Entity: make(slots, 27)}
	for _, name := range []string{"minecraft:red_shulker_box", "minecraft:light_gray_shulker_box"} {
		shulker := protocol.ItemStack{ItemID: itemID(t, name), Count: 1}
		if view.placeable(0, shulker) {
			t.Errorf("%s into a shulker box window's own slots, want it refused", name)
		}
	}
}
