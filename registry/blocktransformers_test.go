package registry

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/nbt"
)

func TestBlockTransformerStackCodec(t *testing.T) {
	for _, tc := range []struct {
		item Item
		want BlockTransformer
	}{
		{ItemWoodenAxe, BlockTransformerAxe},
		{ItemIronHoe, BlockTransformerHoe},
		{ItemDiamondShovel, BlockTransformerShovel},
		{ItemStick, ""},
	} {
		stack := protocol.ItemStack{ItemID: int32(tc.item), Count: 1}
		if got, ok := StackBlockTransformer(stack); got != tc.want || ok != (tc.want != "") {
			t.Fatalf("item %d default = %s, %v; want %s", tc.item, got, ok, tc.want)
		}
		for id, transformer := range []BlockTransformer{BlockTransformerAxe, BlockTransformerHoe, BlockTransformerShovel} {
			if !ReadComponent(&stack, "minecraft:block_transformer", nbt.String(transformer)) {
				t.Fatal("refused transformer", transformer)
			}
			if got, ok := stack.BlockTransformerOf(); !ok || got != int32(id) {
				t.Fatalf("holder = %d, %v; want %d", got, ok, id)
			}
			if got, ok := StackBlockTransformer(stack); !ok || got != transformer {
				t.Fatalf("patched transformer = %s, %v; want %s", got, ok, transformer)
			}
			if tag := StackComponents(stack)["minecraft:block_transformer"]; tag != nbt.String(transformer) {
				t.Fatalf("saved transformer = %v, want %s", tag, transformer)
			}
			again, ok := StoredStack(StackTag(stack))
			if !ok || !again.Equal(stack) {
				t.Fatal("transformer did not survive NBT")
			}
			stack.Remove(protocol.DataComponentBlockTransformer)
			if _, ok := StackBlockTransformer(stack); ok {
				t.Fatal("removed transformer resolved")
			}
			again, ok = StoredStack(StackTag(stack))
			if !ok || !again.Equal(stack) {
				t.Fatal("transformer removal did not survive NBT")
			}
		}
		for _, value := range []nbt.Tag{nbt.String("missing:axe"), nbt.Int(0), nbt.Compound{}} {
			if ReadComponent(&stack, "minecraft:block_transformer", value) {
				t.Errorf("accepted invalid transformer %v", value)
			}
		}
	}
}
