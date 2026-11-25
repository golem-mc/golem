package entity

import (
	"math/rand"
	"testing"
)

// BenchmarkGoatJumpCandidate measures selection with the same candidates and
// random stream each time; world construction and settling are outside timing.
func BenchmarkGoatJumpCandidate(b *testing.B) {
	for _, layout := range []string{"gap", "platform", "frog-platform"} {
		b.Run(layout, func(b *testing.B) {
			w := newTestWorld()
			stone := defaultState(b, "minecraft:stone")
			for x := int32(-8); x <= 10; x++ {
				for z := int32(-8); z <= 8; z++ {
					if layout != "gap" || z >= -3 && z <= 3 && (x >= -3 && x <= 2 || x >= 6) {
						w.set(x, 63, z, stone)
					}
				}
			}
			var m *Mob
			if layout == "frog-platform" {
				m = NewMob(TypeFrog, 1, 2.5, 64, 0.5, 1)
				m.tick(w, nil, []*Mob{m})
				m.SetFrogLongJumpCooldown(0)
			} else {
				m = settledGoat(w, 1)
			}
			g := goatJumpGoal{m: m}
			view := tickView{w: w}
			g.Start(&view)
			j := m.longJump()
			if layout == "frog-platform" {
				j.wantPreferred = true
			}
			initial := *j
			candidates := append([]jumpSpot(nil), initial.candidates...)
			rng := rand.New(rand.NewSource(1))
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				*j = initial
				copy(j.candidates, candidates)
				rng.Seed(1)
				m.Rand = rng
				g.pick(&view)
			}
		})
	}
}
