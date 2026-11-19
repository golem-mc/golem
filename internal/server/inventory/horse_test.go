package inventory

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
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

// saddleOnly is a mount that wears no body armour, as a donkey.
type saddleOnly struct{ horseChest }

func (*saddleOnly) Fits(slot int, _ protocol.ItemStack) bool { return slot != HorseArmorSlot }
