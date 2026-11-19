package entity

import "testing"

// BenchmarkThrownWaterStep measures a snowball's submerged flight tick.
func BenchmarkThrownWaterStep(b *testing.B) {
	w := newTestWorld()
	water := defaultState(b, "minecraft:water")
	for x := int32(-1); x <= 1; x++ {
		for y := int32(64); y <= 67; y++ {
			for z := int32(-1); z <= 1; z++ {
				w.set(x, y, z, water)
			}
		}
	}
	p := NewThrown(1, ThrownSnowball, 0, 65, 0, .25, .3, .15, 0, 1)
	p.Step(w, nil, nil)
	b.ReportAllocs()
	for b.Loop() {
		p.X, p.Y, p.Z = 0, 65, 0
		p.VX, p.VY, p.VZ = .25, .3, .15
		p.Step(w, nil, nil)
	}
	if p.Dead {
		b.Fatal("snowball hit something")
	}
}
