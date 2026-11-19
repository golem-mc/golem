package entity

import (
	"testing"

	"github.com/golem-mc/golem/registry"
)

// BenchmarkProjectileWaterTick measures the three flight paths that check water entry.
func BenchmarkProjectileWaterTick(b *testing.B) {
	for _, wet := range []bool{false, true} {
		name := "air"
		w := newTestWorld()
		if wet {
			name = "water"
			w.set(0, 64, 0, registry.BlockWater.DefaultState())
		}
		b.Run(name, func(b *testing.B) {
			b.Run("trident", func(b *testing.B) {
				a := newTestTrident(7)
				a.Loyalty = 3
				a.RestoreDealtDamage(true)
				owner := PlayerTarget(7, 10, 64, 0, false)
				b.ReportAllocs()
				for b.Loop() {
					a.X, a.Y, a.Z = 0.5, 64, 0.5
					a.VX, a.VY, a.VZ = 0, 0, 0
					a.Step(&w, &owner, nil)
				}
			})
			b.Run("firework", func(b *testing.B) {
				f := NewFirework(1, 0.5, 64, 0.5, 0, 0, 0, 0, 3)
				f.ShotAtAngle = true
				f.Lifetime = 100
				b.ReportAllocs()
				for b.Loop() {
					f.Life = 1
					f.Step(&w, nil, nil, nil)
					f.X, f.Y, f.Z = 0.5, 64, 0.5
				}
			})
			b.Run("eye", func(b *testing.B) {
				e := NewEyeOfEnder(1, 0.5, 64, 0.5)
				b.ReportAllocs()
				for b.Loop() {
					e.life = 1
					e.Step(&w)
				}
			})
		})
	}
}
