package registry

import "testing"

func BenchmarkEventLookups(b *testing.B) {
	b.Run("furnace", func(b *testing.B) {
		for b.Loop() {
			_ = FuelBurnTicks(int32(ItemOakPlanks))
		}
	})
	b.Run("empty-brewing-fuel", func(b *testing.B) {
		for b.Loop() {
			_ = BrewingFuelUses(int32(ItemGlassBottle))
		}
	})
	b.Run("particle", func(b *testing.B) {
		for b.Loop() {
			_, _, _ = ParticleID("minecraft:witch")
		}
	})
}
