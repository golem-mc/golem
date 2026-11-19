package registry

import (
	"slices"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/nbt"
)

func TestEnchantmentTraversalAcrossMutationAndWire(t *testing.T) {
	for _, stored := range []bool{false, true} {
		name := "minecraft:enchantments"
		if stored {
			name = "minecraft:stored_enchantments"
		}
		input, _ := nbt.ParseOrderedSNBT(`{"minecraft:lure":1,"minecraft:riptide":3,"minecraft:binding_curse":5,"minecraft:frost_walker":4,"minecraft:quick_charge":2}`)
		stack := protocol.ItemStack{ItemID: int32(ItemStick), Count: 1}
		if !ReadComponent(&stack, name, input) {
			t.Fatal("fixture refused")
		}
		ids := stack.EnchantmentIDs(stored)
		copy := stack
		copy.SetEnchantment(stored, ids[0], 9)
		copy.SetEnchantment(stored, ids[1], 0)
		copy.SetEnchantment(stored, ids[1], 3)
		want := append(slices.Delete(slices.Clone(ids), 1, 2), ids[1])
		if !slices.Equal(copy.EnchantmentIDs(stored), want) || !slices.Equal(stack.EnchantmentIDs(stored), ids) {
			t.Fatal("edit lost traversal or mutated original")
		}
		if !nbt.Equal(enchantmentsTag(stack, stored), input) {
			t.Fatal("copied stack edit reached original levels")
		}
		for _, untrusted := range []bool{false, true} {
			var data protocol.Buffer
			if untrusted {
				data.PutUntrustedSlot(copy)
			} else {
				data.PutSlot(copy)
			}
			r := protocol.NewReader(data.Bytes())
			var got protocol.ItemStack
			var ok bool
			if untrusted {
				got, ok = r.ReadUntrustedSlot()
			} else {
				got, ok = r.ReadSlot()
			}
			if !ok || r.Err() != nil || !slices.Equal(got.EnchantmentIDs(stored), want) || !got.Same(copy) {
				t.Fatalf("wire traversal: %v, %v", got.EnchantmentIDs(stored), r.Err())
			}
			loaded, ok := StoredStack(StackTag(got))
			if !ok || !slices.Equal(nbt.HashOrderedKeys(enchantmentsTag(loaded, stored)), nbt.HashOrderedKeys(enchantmentsTag(copy, stored))) {
				t.Fatal("save round trip lost collision history")
			}
		}
		levels := map[int32]int32{ids[0]: 7}
		copy.SetEnchantments(stored, levels, ids[0], ids[0])
		levels[ids[0]] = 8
		key, _ := EnchantmentName(ids[0])
		if len(copy.EnchantmentIDs(stored)) != 1 || enchantmentsTag(copy, stored).Entries()[key] != nbt.Int(7) {
			t.Fatal("replacement aliases input or repeats a key")
		}
	}
}
