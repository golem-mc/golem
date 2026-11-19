package registry

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/nbt"
)

func TestFuelStoredStack(t *testing.T) {
	t.Parallel()
	for _, patch := range []nbt.Compound{
		{"minecraft:cooking_fuel": nbt.Compound{"burn_time": nbt.Int(1600), "speed_multiplier": nbt.Float(1)}},
		{"minecraft:cooking_fuel": nbt.Compound{"burn_time": nbt.String("minecraft:cooking/time_coal"), "speed_multiplier": nbt.String("minecraft:cooking/speed_default")}},
		{"minecraft:brewing_fuel": nbt.Compound{"uses": nbt.Int(5), "speed_multiplier": nbt.Float(2)}},
		{"!minecraft:cooking_fuel": nbt.Compound{}},
		{"!minecraft:brewing_fuel": nbt.Compound{}},
	} {
		stack, ok := StoredStack(nbt.Compound{"id": nbt.String("minecraft:stick"), "count": nbt.Int(2), "components": patch})
		if !ok {
			t.Fatalf("refused patch %v", patch)
		}
		back, ok := StoredStack(StackTag(stack))
		if !ok || !stack.Equal(back) {
			t.Errorf("save round trip = %+v, %v, want %+v", back, ok, stack)
		}
		var b protocol.Buffer
		b.PutSlot(back)
		back, ok = protocol.NewReader(b.Bytes()).ReadSlot()
		if !ok || !stack.Equal(back) {
			t.Errorf("wire after save = %+v, %v", back, ok)
		}
	}
	coal := protocol.ItemStack{ItemID: int32(ItemCoal), Count: 1}
	if !HasCookingFuel(coal) || HasBrewingFuel(coal) {
		t.Error("coal's default fuel is wrong")
	}
	coal.Remove(protocol.DataComponentCookingFuel)
	if HasCookingFuel(coal) {
		t.Error("a removed cooking_fuel still counts")
	}
	stack := protocol.ItemStack{ItemID: int32(ItemStick), Count: 1}
	if ReadComponent(&stack, "minecraft:cooking_fuel", nbt.Compound{"burn_time": nbt.Int(1)}) {
		t.Error("accepted a cooking_fuel without speed_multiplier")
	}
}
