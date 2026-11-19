package registry

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/nbt"
)

func TestUseRemainderStoredCodec(t *testing.T) {
	t.Parallel()
	for _, value := range []nbt.Tag{
		nbt.String("stick"),
		nbt.Compound{"id": nbt.String("stick"), "count": nbt.Byte(2), "components": nbt.Compound{
			"minecraft:custom_name": nbt.String("Leftovers"), "!minecraft:food": nbt.Compound{},
		}},
	} {
		stack := protocol.ItemStack{ItemID: int32(ItemApple), Count: 1}
		if !ReadComponent(&stack, "minecraft:use_remainder", value) {
			t.Fatalf("refused valid template %v", value)
		}
		saved, ok := StoredStack(StackTag(stack))
		got, has := saved.UseRemainderOf()
		want, _ := stack.UseRemainderOf()
		if !ok || !has || !got.Same(want) || got.Count != want.Count {
			t.Fatalf("stored remainder = %+v, want %+v", got, want)
		}
		use, ok := StackConsumable(saved)
		if !ok || !use.Remainder.Same(want) || use.Remainder.Count != want.Count {
			t.Fatalf("resolved remainder = %+v, want %+v", use.Remainder, want)
		}
	}
	for _, value := range []nbt.Tag{
		nbt.String("missing"), nbt.String("air"), nbt.Int(1), nbt.Compound{},
		nbt.Compound{"id": nbt.String("stick"), "count": nbt.Int(0)},
		nbt.Compound{"id": nbt.String("stick"), "count": nbt.Int(100)},
		nbt.Compound{"id": nbt.String("stick"), "components": nbt.Int(1)},
	} {
		stack := protocol.ItemStack{ItemID: int32(ItemApple), Count: 1}
		if ReadComponent(&stack, "minecraft:use_remainder", value) {
			t.Errorf("accepted invalid template %v", value)
		}
	}
}
