package entity

import "testing"

// BenchmarkMobTravelInWater measures horizontal drag and collision movement
// for the default and overridden water slowdowns.
func BenchmarkMobTravelInWater(b *testing.B) {
	for _, kind := range []MobType{TypeCow, TypePolarBear, TypeSkeletonHorse} {
		b.Run(kind.Name(), func(b *testing.B) {
			w := newTestWorld()
			water := defaultState(b, "minecraft:water")
			for x := int32(-2); x <= 3; x++ {
				for y := int32(64); y <= 67; y++ {
					for z := int32(-2); z <= 3; z++ {
						w.set(x, y, z, water)
					}
				}
			}
			m := NewMob(kind, 1, 0.5, 65, 0.5, 1)
			m.updateMobFluids(w)
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				m.X, m.Y, m.Z = 0.5, 65, 0.5
				m.VX, m.VY, m.VZ = 1, 0, 1
				m.travelInWater(w, mobGravity, true, m.Y)
			}
		})
	}
}
