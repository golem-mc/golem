package inventory

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/registry"
)

// BenchmarkHopperItemRules exercises the item checks in sided hopper transfers.
func BenchmarkHopperItemRules(b *testing.B) {
	for _, tc := range []struct {
		name    string
		kind    Kind
		slots   int
		face    int32
		item    registry.Item
		accepts bool
	}{
		{"FurnaceBucket", KindFurnace, 3, protocol.FaceEast, registry.ItemBucket, true},
		{"BrewingIngredient", KindBrewingStand, 5, protocol.FaceTop, registry.ItemNetherWart, true},
		{"BrewingBottle", KindBrewingStand, 5, protocol.FaceEast, registry.ItemPotion, true},
		{"ShulkerRefusal", KindShulker, 27, protocol.FaceTop, registry.ItemRedShulkerBox, false},
	} {
		b.Run(tc.name, func(b *testing.B) {
			hopper := &testBlock{kind: KindHopper, slots: make([]protocol.ItemStack, 5)}
			dest := &testBlock{kind: tc.kind, slots: make([]protocol.ItemStack, tc.slots)}
			stack := protocol.ItemStack{ItemID: int32(tc.item), Count: 1}
			b.ReportAllocs()
			for b.Loop() {
				hopper.slots[0] = stack
				moved, _ := Eject(hopper, dest, tc.face)
				if moved != tc.accepts {
					b.Fatal("unexpected transfer result")
				}
				clear(dest.slots)
			}
		})
	}
}
