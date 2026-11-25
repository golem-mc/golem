package inventory

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
)

func BenchmarkCrafterSingleHopperInsert(b *testing.B) {
	stone := item(b, "minecraft:stone", 1)
	dirt := item(b, "minecraft:dirt", 32)
	for _, tc := range []struct {
		name  string
		slot  int
		start protocol.ItemStack
		moved bool
	}{
		{name: "first_empty", slot: 0, moved: true},
		{name: "last_empty", slot: 8, moved: true},
		{name: "last_merge", slot: 8, start: stone, moved: true},
		{name: "rejected", slot: 8, start: dirt},
	} {
		b.Run(tc.name, func(b *testing.B) {
			crafter := &testBlock{kind: KindCrafter, slots: make([]protocol.ItemStack, 9)}
			for i := range crafter.slots {
				crafter.slots[i] = dirt
			}
			want := tc.start
			if tc.moved {
				want.ItemID = stone.ItemID
				want.Count++
			}
			b.ReportAllocs()
			for b.Loop() {
				crafter.Put(tc.slot, tc.start)
				moved, intoEmpty := addOneItem(crafter, &stone, protocol.FaceTop, true)
				got := crafter.StackPtr(tc.slot)
				if moved != tc.moved || intoEmpty || got.ItemID != want.ItemID || got.Count != want.Count {
					b.Fatalf("moved %v, intoEmpty %v, slot %d = %v, want moved %v, intoEmpty false, slot = %v",
						moved, intoEmpty, tc.slot, got, tc.moved, want)
				}
			}
		})
	}
}
