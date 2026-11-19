package entity

import (
	"math/rand"
	"testing"
)

// BenchmarkGoatJumpCandidate measures selection with the same candidates and
// random stream each time; world construction and settling are outside timing.
func BenchmarkGoatJumpCandidate(b *testing.B) {
	for _, layout := range []string{"gap", "platform"} {
		b.Run(layout, func(b *testing.B) {
			w := newTestWorld()
			stone := defaultState(b, "minecraft:stone")
			for x := int32(-8); x <= 10; x++ {
				for z := int32(-8); z <= 8; z++ {
					if layout == "platform" || z >= -3 && z <= 3 && (x >= -3 && x <= 2 || x >= 6) {
						w.set(x, 63, z, stone)
					}
				}
			}
			m := settledGoat(w, 1)
			g := goatJumpGoal{m: m}
			view := tickView{w: w}
			g.Start(&view)
			initial := m.Goat.jump
			candidates := append([]jumpSpot(nil), initial.candidates...)
			rng := rand.New(rand.NewSource(1))
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				m.Goat.jump = initial
				copy(m.Goat.jump.candidates, candidates)
				rng.Seed(1)
				m.Rand = rng
				g.pick(&view)
			}
		})
	}
}
