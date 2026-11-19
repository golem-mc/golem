package protocol

import (
	"slices"
	"testing"

	"github.com/golem-mc/golem/nbt"
)

// TestSlotCarriesLoreCustomDataAndAttributes checks that the components a
// loot function lays on a stack survive the slot codec the client reads.
func TestSlotCarriesLoreCustomDataAndAttributes(t *testing.T) {
	s := ItemStack{ItemID: 1, Count: 1}
	s.SetLore([]Text{{Text: "a"}, {Text: "b"}})
	s.SetCustomData(nbt.Compound{"k": nbt.Int(7)})
	s.SetAttributeModifiers([]AttributeModifierEntry{{Attribute: 1, ID: "x:y", Amount: 1.5, Operation: 2, Slot: 3}})
	s.SetTooltipDisplay(true, []int32{DataComponentEnchantments})

	var b Buffer
	b.PutSlot(s)
	got, ok := NewReader(b.Bytes()).ReadSlot()
	if !ok {
		t.Fatal("the stack did not read back")
	}
	if lore := got.LoreOf(); len(lore) != 2 || lore[1].Text != "b" {
		t.Errorf("lore = %v", lore)
	}
	if got.CustomData()["k"] != nbt.Int(7) {
		t.Errorf("custom data = %v", got.CustomData())
	}
	if e, ok := got.AttributeModifiersOf(); !ok || len(e) != 1 || e[0].Amount != 1.5 || e[0].Slot != 3 {
		t.Errorf("attribute modifiers = %v", e)
	}
	if hide, hidden := got.HiddenTooltipOf(); !hide || !slices.Equal(hidden, []int32{DataComponentEnchantments}) {
		t.Errorf("tooltip display = %v %v", hide, hidden)
	}

	// An entry with override text (display 2) passes the slot codec but
	// cannot be held by an entry, so the accessor reports none.
	var v Buffer
	v.PutVarInt(1)
	v.PutVarInt(1)
	v.PutString("x:y")
	v.PutF64(1)
	v.PutVarInt(0)
	v.PutVarInt(0)
	v.PutVarInt(2)
	v.PutText(Text{Text: "shown"})
	o := ItemStack{ItemID: 1, Count: 1}
	o.putRaw(RawComponent{Type: DataComponentAttributeModifiers, Data: v.Bytes()})
	var ob Buffer
	ob.PutSlot(o)
	read, ok := NewReader(ob.Bytes()).ReadSlot()
	if !ok {
		t.Fatal("a stack with override text did not read back")
	}
	if e, ok := read.AttributeModifiersOf(); ok || e != nil {
		t.Errorf("override-text modifiers = %v, %v", e, ok)
	}
}
