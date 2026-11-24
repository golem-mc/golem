package command

import (
	"testing"

	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// TestItemSlotsArmorRange checks armor.* names the body slot beside the four
// the 100s band covers.
func TestItemSlotsArmorRange(t *testing.T) {
	slots, ok := itemSlots("armor.*")
	if !ok {
		t.Fatal("armor.* is not a known slot word")
	}
	want := map[ItemSlot]bool{
		ItemSlot(103): true, // head
		ItemSlot(102): true, // chest
		ItemSlot(101): true, // legs
		ItemSlot(100): true, // feet
		ItemSlot(105): true, // body
	}
	if len(slots) != len(want) {
		t.Fatalf("armor.* names %v, want the five armour slots", slots)
	}
	for _, slot := range slots {
		if !want[slot] {
			t.Errorf("armor.* names %d, want only the armour slots", slot)
		}
	}
}

// TestItemModifierSetNameTargets checks that set_name reads its target.
// vanilla: SetNameFunction.CODEC
func TestItemModifierSetNameTargets(t *testing.T) {
	for snbt, want := range map[string]bool{
		`{type:"set_name",name:"Hi",target:"item_name"}`:   true,
		`{type:"set_name",name:"Hi",target:"custom_name"}`: false,
		`{type:"set_name",name:"Hi"}`:                      false,
	} {
		tag, err := nbt.ParseSNBT(snbt)
		if err != nil {
			t.Fatal(err)
		}
		fn, msg, _ := decodeItemModifier(tag)
		got, ok := fn.(registry.LootSetName)
		if msg != "" || !ok || got.ItemName != want {
			t.Errorf("%s: got %#v (%q), want ItemName %v", snbt, fn, msg, want)
		}
	}
}
