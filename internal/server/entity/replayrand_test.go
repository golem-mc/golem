package entity

import (
	"testing"
)

// A copied body that is rewound to its mark draws the same rolls again, as a
// held ProjectileHitEvent step needs.
func TestReplayRandRewindRepeatsRolls(t *testing.T) {
	a := NewArrow(1, 0, 0, 0, 0, 0, 0, true, 2, PickupAllowed, 0, 42)
	th := NewThrown(2, ThrownEgg, 0, 0, 0, 0, 0, 0, 0, 42)
	for _, tc := range []struct {
		name string
		draw func() int
		mark func() uint64
		back func(uint64)
	}{
		{"arrow", func() int { return a.Rand.Intn(1000) + int(a.Rand.Float32()*1000) }, a.RandSrc.Mark, a.RandSrc.Rewind},
		{"thrown", func() int { return th.Rand.Intn(1000) + int(th.Rand.Float32()*1000) }, th.RandSrc.Mark, th.RandSrc.Rewind},
	} {
		tc.draw()
		mark := tc.mark()
		first := []int{tc.draw(), tc.draw(), tc.draw()}
		tc.back(mark)
		for i, want := range first {
			if got := tc.draw(); got != want {
				t.Fatalf("%s: draw %d after rewind = %d, want %d", tc.name, i, got, want)
			}
		}
	}
}
