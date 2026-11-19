package inventory

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/registry"
)

// horseChest is a mount's saddle, armour and chest slots.
type horseChest [17]protocol.ItemStack

func (c *horseChest) Slot(i int) protocol.ItemStack   { return c[i] }
func (c *horseChest) Put(i int, s protocol.ItemStack) { c[i] = s }

func horseView(columns int32) (View, *horseChest) {
	var w Window
	v := w.View(KindHorse, 1)
	v.Columns = columns
	c := &horseChest{}
	v.Entity = c
	return v, c
}

func TestHorseWindowSlotCount(t *testing.T) {
	for _, tc := range []struct{ columns, want int32 }{{0, 38}, {3, 47}, {5, 53}} {
		v, _ := horseView(tc.columns)
		if got := v.Slots(); got != tc.want {
			t.Errorf("columns %d: %d slots, want %d", tc.columns, got, tc.want)
		}
		if got := v.InvBase(); got != 2+3*tc.columns {
			t.Errorf("columns %d: inventory base %d", tc.columns, got)
		}
	}
}

func TestHorseSaddleAndArmourSlotRules(t *testing.T) {
	v, _ := horseView(0)
	saddle := protocol.ItemStack{ItemID: itemID(t, "minecraft:saddle"), Count: 1}
	armour := protocol.ItemStack{ItemID: itemID(t, "minecraft:iron_horse_armor"), Count: 1}
	stone := protocol.ItemStack{ItemID: itemID(t, "minecraft:stone"), Count: 1}
	for _, tc := range []struct {
		slot  int32
		stack protocol.ItemStack
		want  bool
	}{
		{0, saddle, true}, {0, armour, false}, {0, stone, false},
		{1, armour, true}, {1, saddle, false}, {1, stone, false},
		{2, stone, true},
	} {
		if got := v.placeable(tc.slot, tc.stack); got != tc.want {
			t.Errorf("slot %d item %d: placeable %t, want %t", tc.slot, tc.stack.ItemID, got, tc.want)
		}
	}
	if got := v.slotLimit(0, saddle); got != 1 {
		t.Errorf("saddle slot holds %d", got)
	}
}

func TestHorseChestSlotsOnlyWithAChest(t *testing.T) {
	stone := protocol.ItemStack{ItemID: itemID(t, "minecraft:stone"), Count: 1}
	plain, _ := horseView(0)
	if loc, _ := plain.resolve(2); loc != slotInv {
		t.Errorf("slot 2 of a mount without a chest is %d, want the player's things", loc)
	}
	donkey, _ := horseView(5)
	if loc, index := donkey.resolve(16); loc != slotEntity || index != 16 {
		t.Errorf("slot 16 = %d/%d, want the last chest slot", loc, index)
	}
	if !donkey.placeable(10, stone) {
		t.Error("a chest slot refused a stone")
	}
}

func TestHorseShiftClickFillsArmourThenSaddleThenChest(t *testing.T) {
	v, w := horseView(3)
	inv := &player.Inventory{}
	saddle := protocol.ItemStack{ItemID: itemID(t, "minecraft:saddle"), Count: 1}
	stone := protocol.ItemStack{ItemID: itemID(t, "minecraft:stone"), Count: 4}
	base := v.InvBase()
	inv.Set(player.MainSlot, saddle)
	inv.Set(player.MainSlot+1, stone)
	session := &Session{}
	var out Outcome
	session.Click(v, inv, protocol.ClickModeQuickMove, 0, base, false, 0, &out)
	if w[HorseSaddleSlot].Empty() {
		t.Error("the shift-clicked saddle did not reach the saddle slot")
	}
	out = Outcome{}
	session.Click(v, inv, protocol.ClickModeQuickMove, 0, base+1, false, 0, &out)
	if got := v.Entity.Slot(HorseBaseSlots); got.Count != 4 {
		t.Errorf("stone went to chest slot 0 as %+v", got)
	}
}

func TestHorseShiftClickFromFullChestFallsBackToHotbar(t *testing.T) {
	v, _ := horseView(3)
	inv := &player.Inventory{}
	stone := protocol.ItemStack{ItemID: itemID(t, "minecraft:stone"), Count: 64}
	dirt := protocol.ItemStack{ItemID: itemID(t, "minecraft:dirt"), Count: 64}
	for i := range 9 {
		v.Entity.Put(HorseBaseSlots+i, stone)
	}
	base := v.InvBase()
	inv.Set(player.MainSlot, dirt)
	session := &Session{}
	var out Outcome
	session.Click(v, inv, protocol.ClickModeQuickMove, 0, base, false, 0, &out)
	if got := inv.Slots[player.HotbarSlot]; got.Count != 64 {
		t.Errorf("first hotbar slot = %+v, want the dirt swapped down", got)
	}
	if !inv.Slots[player.MainSlot].Empty() {
		t.Error("the dirt stayed in the main inventory")
	}
}

func TestHorseShiftClickArmourBeforeSaddle(t *testing.T) {
	v, w := horseView(0)
	inv := &player.Inventory{}
	armour := protocol.ItemStack{ItemID: itemID(t, "minecraft:iron_horse_armor"), Count: 1}
	inv.Set(player.MainSlot, armour)
	session := &Session{}
	var out Outcome
	session.Click(v, inv, protocol.ClickModeQuickMove, 0, v.InvBase(), false, 0, &out)
	if w[HorseArmorSlot].Empty() || !w[HorseSaddleSlot].Empty() {
		t.Errorf("armour went to %+v", w[:2])
	}
}

func TestHorseShiftClickOutOfSaddleAndChest(t *testing.T) {
	v, w := horseView(3)
	inv := &player.Inventory{}
	saddle := protocol.ItemStack{ItemID: itemID(t, "minecraft:saddle"), Count: 1}
	stone := protocol.ItemStack{ItemID: itemID(t, "minecraft:stone"), Count: 3}
	w[HorseSaddleSlot] = saddle
	v.Entity.Put(HorseBaseSlots, stone)
	session := &Session{}
	var out Outcome
	session.Click(v, inv, protocol.ClickModeQuickMove, 0, HorseSaddleSlot, false, 0, &out)
	session.Click(v, inv, protocol.ClickModeQuickMove, 0, HorseBaseSlots, false, 0, &out)
	if !w[HorseSaddleSlot].Empty() || !v.Entity.Slot(HorseBaseSlots).Empty() {
		t.Error("the saddle or the chest stone stayed")
	}
}

// saddleOnly is a mount that wears no body armour, as a donkey.
type saddleOnly struct{ horseChest }

func (*saddleOnly) Fits(slot int, _ protocol.ItemStack) bool { return slot != HorseArmorSlot }

func TestHorseArmourSlotAsksTheMount(t *testing.T) {
	v, _ := horseView(0)
	v.Entity = &saddleOnly{}
	armour := protocol.ItemStack{ItemID: itemID(t, "minecraft:iron_horse_armor"), Count: 1}
	saddle := protocol.ItemStack{ItemID: itemID(t, "minecraft:saddle"), Count: 1}
	if v.placeable(HorseArmorSlot, armour) {
		t.Error("a mount that wears no armour took horse armour")
	}
	if !v.placeable(HorseSaddleSlot, saddle) {
		t.Error("the saddle slot refused a saddle")
	}
}

// TestBindingCurseKeepsMountArmourOn: a survival player cannot take a cursed
// saddle or body armour out of the mount screen; creative can.
// vanilla: ArmorSlot.mayPickup
func TestBindingCurseKeepsMountArmourOn(t *testing.T) {
	curse, ok := registry.EnchantmentID("minecraft:binding_curse")
	if !ok {
		t.Fatal("no binding_curse enchantment")
	}
	for _, tc := range []struct {
		slot int32
		item string
	}{{HorseSaddleSlot, "minecraft:saddle"}, {HorseArmorSlot, "minecraft:iron_horse_armor"}} {
		cursed := protocol.ItemStack{ItemID: itemID(t, tc.item), Count: 1, Enchantments: map[int32]int32{curse: 1}}
		for _, creative := range []bool{false, true} {
			v, w := horseView(0)
			w[tc.slot] = cursed
			inv := &player.Inventory{}
			session := &Session{}
			var out Outcome
			session.Click(v, inv, protocol.ClickModeQuickMove, 0, tc.slot, creative, 0, &out)
			kept := w[tc.slot].Equal(cursed)
			if kept == creative {
				t.Errorf("%s creative=%t: kept=%t", tc.item, creative, kept)
			}
		}
	}
}
