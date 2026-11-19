package inventory

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/registry"
)

// TestEveryArmourSlotHoldsOne checks the cap that covers the whole armour
// range: each of the four slots a piece is worn in holds one, and the main
// inventory slot right after them keeps the item's own whole stack.
// vanilla: ArmorSlot.getMaxStackSize
func TestEveryArmourSlotHoldsOne(t *testing.T) {
	pumpkins := protocol.ItemStack{ItemID: itemID(t, "minecraft:carved_pumpkin"), Count: 64}
	view := View{Kind: KindPlayer}
	for slot := int32(player.ArmorSlot); slot < player.ArmorSlot+player.ArmorSlots; slot++ {
		if got := view.slotLimit(slot, pumpkins); got != 1 {
			t.Errorf("armour slot %d limit = %d, want 1", slot, got)
		}
	}
	if got := view.slotLimit(player.MainSlot, pumpkins); got != 64 {
		t.Errorf("the slot after the armour limit = %d, want the whole stack", got)
	}
}

// TestShiftClickEquipsArmourFromThePlayersThings checks the piece of
// armour a shift-click moves out of the player's own things: it tries
// the armour slot its equippable component names first - vanilla:
// InventoryMenu.quickMoveStack - and falls in between the main
// inventory and the hotbar only when that slot is taken.
func TestShiftClickEquipsArmourFromThePlayersThings(t *testing.T) {
	chestplate := protocol.ItemStack{ItemID: itemID(t, "minecraft:iron_chestplate"), Count: 1}
	helmet := protocol.ItemStack{ItemID: itemID(t, "minecraft:diamond_helmet"), Count: 1}

	// A chestplate in the main inventory goes straight onto its slot.
	inv := &player.Inventory{}
	inv.Set(player.MainSlot, chestplate)
	var outcome Outcome
	session := &Session{}
	session.Click(View{}, inv, protocol.ClickModeQuickMove, 0, player.MainSlot, false, 0, &outcome)
	if got := inv.Slots[player.ArmorSlot+1]; !got.Equal(chestplate) {
		t.Errorf("the chest slot holds %+v, want the chestplate shifted onto it", got)
	}
	if !inv.Slots[player.MainSlot].Empty() {
		t.Errorf("main inventory slot 9 = %+v, want it emptied by the move", inv.Slots[player.MainSlot])
	}
	if !outcome.Visible {
		t.Error("a shift-click that put armour on was not marked for the equipment broadcast")
	}

	// One in the hotbar goes the same way, onto the slot it belongs in.
	inv = &player.Inventory{}
	inv.Set(player.HotbarSlot, helmet)
	outcome = Outcome{}
	session = &Session{}
	session.Click(View{}, inv, protocol.ClickModeQuickMove, 0, player.HotbarSlot, false, 0, &outcome)
	if got := inv.Slots[player.ArmorSlot]; !got.Equal(helmet) {
		t.Errorf("the head slot holds %+v, want the helmet shifted onto it", got)
	}
	if !inv.Slots[player.HotbarSlot].Empty() {
		t.Errorf("hotbar slot 36 = %+v, want it emptied by the move", inv.Slots[player.HotbarSlot])
	}

	// Its slot taken, the piece falls in between the main inventory and
	// the hotbar: nothing is forced onto the worn one.
	inv = &player.Inventory{}
	inv.Set(player.ArmorSlot+1, chestplate)
	inv.Set(player.MainSlot, chestplate)
	outcome = Outcome{}
	session = &Session{}
	session.Click(View{}, inv, protocol.ClickModeQuickMove, 0, player.MainSlot, false, 0, &outcome)
	if got := inv.Slots[player.HotbarSlot]; !got.Equal(chestplate) {
		t.Errorf("hotbar slot 36 = %+v, want the second chestplate the full armour slot left", got)
	}
	if got := inv.Slots[player.ArmorSlot+1]; !got.Equal(chestplate) {
		t.Errorf("the chest slot holds %+v, want the worn chestplate untouched", got)
	}

	// And a piece already worn moves the other way, into the main
	// inventory and the hotbar - which is where the vanilla server's
	// quickMoveStack sends the armour slots' stacks.
	inv = &player.Inventory{}
	inv.Set(player.ArmorSlot, helmet)
	outcome = Outcome{}
	session = &Session{}
	session.Click(View{}, inv, protocol.ClickModeQuickMove, 0, player.ArmorSlot, false, 0, &outcome)
	if got := inv.Slots[player.MainSlot]; !got.Equal(helmet) {
		t.Errorf("main inventory slot 9 = %+v, want the helmet shifted out of the head slot", got)
	}
	if !inv.Slots[player.ArmorSlot].Empty() {
		t.Errorf("the head slot holds %+v, want it emptied by the move", inv.Slots[player.ArmorSlot])
	}
}

// TestBindingCurseKeepsWornArmourOn checks every way a survival player
// takes a worn piece with Curse of Binding off: a click, a shift-click, a
// number-key swap, a drop and a double-click gather all leave it in place, while creative is free
// to.
// vanilla: ArmorSlot.mayPickup
func TestBindingCurseKeepsWornArmourOn(t *testing.T) {
	curse, ok := registry.EnchantmentID("minecraft:binding_curse")
	if !ok {
		t.Fatal("no binding_curse enchantment")
	}
	cursed := protocol.ItemStack{ItemID: itemID(t, "minecraft:iron_helmet"), Count: 1,
		Enchantments: map[int32]int32{curse: 1}}
	other := protocol.ItemStack{ItemID: itemID(t, "minecraft:diamond_helmet"), Count: 1}

	clicks := []struct {
		name         string
		mode, button int32
		hotbar       protocol.ItemStack
	}{
		{"pickup", protocol.ClickModePickup, 0, protocol.ItemStack{}},
		{"half pickup", protocol.ClickModePickup, 1, protocol.ItemStack{}},
		{"shift-click", protocol.ClickModeQuickMove, 0, protocol.ItemStack{}},
		{"swap with an empty hotbar slot", protocol.ClickModeSwap, 0, protocol.ItemStack{}},
		{"swap with a helmet", protocol.ClickModeSwap, 0, other},
		{"swap to the offhand", protocol.ClickModeSwap, 40, protocol.ItemStack{}},
		{"drop", protocol.ClickModeThrow, 1, protocol.ItemStack{}},
	}
	for _, creative := range []bool{false, true} {
		for _, c := range clicks {
			inv := &player.Inventory{}
			inv.Set(player.ArmorSlot, cursed)
			inv.Set(player.HotbarSlot, c.hotbar)
			var outcome Outcome
			session := &Session{}
			session.Click(View{}, inv, c.mode, c.button, player.ArmorSlot, creative, 0, &outcome)
			kept := inv.Slots[player.ArmorSlot].Equal(cursed)
			if !creative && !kept {
				t.Errorf("%s: the cursed helmet came off in survival", c.name)
			}
			if !creative && (len(outcome.Dropped) > 0 || !session.Carried.Empty()) {
				t.Errorf("%s: survival got the cursed helmet out (cursor %+v, dropped %+v)", c.name, session.Carried, outcome.Dropped)
			}
			if creative && kept {
				t.Errorf("%s: creative could not take the cursed helmet off", c.name)
			}
		}
	}

	// A cursed piece in the main inventory moves freely.
	inv := &player.Inventory{}
	inv.Set(player.MainSlot, cursed)
	session := &Session{}
	session.Click(View{}, inv, protocol.ClickModePickup, 0, player.MainSlot, false, 0, &Outcome{})
	if !session.Carried.Equal(cursed) {
		t.Errorf("the cursed helmet in the main inventory did not come up: %+v", session.Carried)
	}

	// The double click that gathers leaves a worn cursed pumpkin alone: it
	// stacks to 64, so the sweep reaches the armour slot.
	pumpkin := protocol.ItemStack{ItemID: itemID(t, "minecraft:carved_pumpkin"), Count: 1,
		Enchantments: map[int32]int32{curse: 1}}
	for _, creative := range []bool{false, true} {
		inv = &player.Inventory{}
		inv.Set(player.ArmorSlot, pumpkin)
		session = &Session{Carried: pumpkin}
		session.Click(View{}, inv, protocol.ClickModeCollect, 0, player.MainSlot, creative, 0, &Outcome{})
		kept := inv.Slots[player.ArmorSlot].Equal(pumpkin)
		if !creative && (!kept || session.Carried.Count != 1) {
			t.Errorf("collect took the worn cursed pumpkin: slot %+v, cursor %+v", inv.Slots[player.ArmorSlot], session.Carried)
		}
		if creative && (kept || session.Carried.Count != 2) {
			t.Errorf("creative collect did not gather the worn pumpkin: slot %+v, cursor %+v", inv.Slots[player.ArmorSlot], session.Carried)
		}
	}
}

// vanilla: BundleContents.Mutable.tryTransfer, ArmorSlot.mayPickup
func TestBundleRespectsBindingCurseOnWornArmour(t *testing.T) {
	curse, ok := registry.EnchantmentID("minecraft:binding_curse")
	if !ok {
		t.Fatal("no binding_curse enchantment")
	}
	helmet := protocol.ItemStack{ItemID: itemID(t, "minecraft:iron_helmet"), Count: 1,
		Enchantments: map[int32]int32{curse: 1}}
	for _, tc := range []struct {
		name     string
		creative bool
	}{
		{"survival", false},
		{"creative", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			inv := &player.Inventory{}
			inv.Set(player.ArmorSlot, helmet)
			session := &Session{Carried: bundleOf(t)}
			var outcome Outcome
			session.Click(View{}, inv, protocol.ClickModePickup, 0, player.ArmorSlot, tc.creative, 0, &outcome)
			wantSound := registry.SoundItemBundleInsertFail
			if tc.creative {
				wantSound = registry.SoundItemBundleInsert
			}
			if len(outcome.BundleSounds) != 1 || outcome.BundleSounds[0] != wantSound {
				t.Errorf("bundle sounds = %v, want [%s]", outcome.BundleSounds, wantSound.Name())
			}

			wantSlot, wantBundle := helmet, bundleOf(t)
			if tc.creative {
				wantSlot = protocol.ItemStack{}
				wantBundle = bundleOf(t, helmet)
			}
			if got := inv.Slots[player.ArmorSlot]; !got.Equal(wantSlot) {
				t.Errorf("armour slot = %+v, want %+v", got, wantSlot)
			}
			if got := session.Carried; !got.Equal(wantBundle) {
				t.Errorf("cursor = %+v, want %+v", got, wantBundle)
			}
		})
	}
}
