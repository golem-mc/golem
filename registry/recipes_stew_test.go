package registry

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
)

func stack(name string, count int32) protocol.ItemStack {
	return protocol.ItemStack{ItemID: mustItemID(name), Count: count}
}

func TestPoppyStewCarriesNightVision(t *testing.T) {
	cells := []protocol.ItemStack{
		stack("minecraft:bowl", 1), stack("minecraft:brown_mushroom", 1),
		stack("minecraft:red_mushroom", 1), stack("minecraft:poppy", 1),
	}
	got := CraftResult(cells, 2)
	if got.ItemID != mustItemID("minecraft:suspicious_stew") || got.Count != 1 {
		t.Fatalf("result = %+v, want one suspicious_stew", got)
	}
	var want protocol.Buffer
	want.PutVarInt(1)
	want.PutVarInt(MustMobEffect("minecraft:night_vision"))
	want.PutVarInt(100)
	if len(got.Raw) != 1 || got.Raw[0].Type != protocol.DataComponentSuspiciousStew ||
		string(got.Raw[0].Data) != string(want.Bytes()) {
		t.Fatalf("raw = %+v, want night_vision for 100 ticks", got.Raw)
	}
}

func TestEightArrowsAroundALingeringPotionImbue(t *testing.T) {
	potion := stack("minecraft:lingering_potion", 1)
	potion.SetPotion(MustPotion("minecraft:night_vision"))
	cells := make([]protocol.ItemStack, 9)
	for i := range cells {
		cells[i] = stack("minecraft:arrow", 1)
	}
	cells[4] = potion
	got := CraftResult(cells, 3)
	if got.ItemID != mustItemID("minecraft:tipped_arrow") || got.Count != 8 {
		t.Fatalf("result = %+v, want 8 tipped_arrow", got)
	}
	contents, ok := got.PotionContentsOf()
	if !ok || contents.Potion != MustPotion("minecraft:night_vision") {
		t.Fatalf("contents = %+v %v, want the night_vision potion", contents, ok)
	}
	cells[0] = protocol.ItemStack{}
	if got := CraftResult(cells, 3); !got.Empty() {
		t.Fatalf("eight-cell grid made %+v, want nothing", got)
	}
}

func TestStewRecipeSecondEffectIsItsOwn(t *testing.T) {
	cells := []protocol.ItemStack{
		stack("minecraft:bowl", 1), stack("minecraft:brown_mushroom", 1),
		stack("minecraft:red_mushroom", 1), stack("minecraft:azure_bluet", 1),
	}
	got := CraftResult(cells, 2)
	var want protocol.Buffer
	want.PutVarInt(1)
	want.PutVarInt(MustMobEffect("minecraft:blindness"))
	want.PutVarInt(220)
	if len(got.Raw) != 1 || string(got.Raw[0].Data) != string(want.Bytes()) {
		t.Fatalf("raw = %+v, want blindness for 220 ticks", got.Raw)
	}
}

func TestStewRefusesGridsMissingAnIngredient(t *testing.T) {
	full := []protocol.ItemStack{
		stack("minecraft:bowl", 1), stack("minecraft:brown_mushroom", 1),
		stack("minecraft:red_mushroom", 1), stack("minecraft:poppy", 1),
	}
	for drop := range full {
		cells := append([]protocol.ItemStack(nil), full...)
		cells[drop] = protocol.ItemStack{}
		if got := CraftResult(cells, 2); got.ItemID == mustItemID("minecraft:suspicious_stew") {
			t.Errorf("grid without cell %d made a stew", drop)
		}
	}
	extra := append(append([]protocol.ItemStack(nil), full...), protocol.ItemStack{}, stack("minecraft:stick", 1))
	if got := CraftResult(extra, 3); got.ItemID == mustItemID("minecraft:suspicious_stew") {
		t.Error("grid with a stray stick made a stew")
	}
}

func TestImbueWithoutPotionContentsLeavesNone(t *testing.T) {
	cells := make([]protocol.ItemStack, 9)
	for i := range cells {
		cells[i] = stack("minecraft:arrow", 1)
	}
	cells[4] = stack("minecraft:lingering_potion", 1)
	got := CraftResult(cells, 3)
	if got.ItemID != mustItemID("minecraft:tipped_arrow") {
		t.Fatalf("result = %+v, want tipped_arrow", got)
	}
	if _, ok := got.PotionContentsOf(); ok {
		t.Error("result carries potion_contents the source lacked")
	}
}
