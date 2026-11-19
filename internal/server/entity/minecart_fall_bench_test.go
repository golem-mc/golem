package entity

import "testing"

// BenchmarkMinecartFallStep measures falling air/fluid ticks and a grounded dry tick.
func BenchmarkMinecartFallStep(b *testing.B) {
	for _, tc := range []struct {
		name        string
		block       string
		y, vy, fall float64
		grounded    bool
	}{
		{name: "minecraft:air", block: "minecraft:air", y: 70.5, vy: -1, fall: 10},
		{name: "minecraft:water", block: "minecraft:water", y: 70.5, vy: -1, fall: 10},
		{name: "minecraft:lava", block: "minecraft:lava", y: 70.5, vy: -1, fall: 10},
		{name: "grounded-dry", block: "minecraft:air", y: 70, grounded: true},
	} {
		b.Run(tc.name, func(b *testing.B) {
			w := newTestWorld()
			state := defaultState(b, tc.block)
			w.set(0, 69, 0, state)
			w.set(0, 70, 0, state)
			if tc.grounded {
				w.set(0, 69, 0, defaultState(b, "minecraft:stone"))
			}
			m := NewMinecart(1, .5, tc.y, .5)
			// Keep the lava case alive so every iteration measures a full tick.
			m.Invulnerable = true
			b.ReportAllocs()
			for b.Loop() {
				m.Y, m.VY, m.fallDistance = tc.y, tc.vy, tc.fall
				m.Step(w, true)
			}
			if m.Dead || (tc.grounded && (!m.OnGround || m.fallDistance != 0)) {
				b.Fatalf("invalid cart state: dead=%v, onGround=%v, fallDistance=%v", m.Dead, m.OnGround, m.fallDistance)
			}
		})
	}
}
