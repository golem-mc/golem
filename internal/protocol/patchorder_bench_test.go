package protocol

import "testing"

// BenchmarkPutPatchOrderedSlot writes a slot, a bundle's templates and a
// template alone, each with several noted components and removals, so the
// history sort runs inside the timed loop.
func BenchmarkPutPatchOrderedSlot(b *testing.B) {
	item := ItemStack{ItemID: 1, Count: 1, Damage: 3, RepairCost: 2, CustomName: &Text{Text: "probe"}, Unbreakable: true}
	item.Remove(DataComponentFood)
	item.Remove(DataComponentTool)
	item.SetEnchantments(false, map[int32]int32{1: 2, 2: 1})
	item.NotePatch(DataComponentDamage, DataComponentFood, DataComponentTool, DataComponentEnchantments)
	children := []ItemStack{item, item, item}
	b.Run("slot", func(b *testing.B) {
		var wire Buffer
		b.ReportAllocs()
		for b.Loop() {
			wire.Reset()
			wire.PutSlot(item)
		}
	})
	b.Run("bundle", func(b *testing.B) {
		var wire Buffer
		b.ReportAllocs()
		for b.Loop() {
			wire.Reset()
			wire.PutBundleContents(children)
		}
	})
	b.Run("template", func(b *testing.B) {
		var wire Buffer
		b.ReportAllocs()
		for b.Loop() {
			wire.Reset()
			wire.PutItemTemplate(item)
		}
	})
}
