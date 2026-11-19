package rules

import "testing"

func TestWithinContainerRangeIsStrict(t *testing.T) {
	// The block's box spans 0..1 on x; eyes on its axis at y and z 0.5.
	for _, c := range []struct {
		rng  float64
		x    float64
		want bool
	}{
		{4.5, 1 + 8.5 - 1e-9, true}, // survival: 4.5 + 4.0
		{4.5, 1 + 8.5, false},
		{5.0, 1 + 9.0 - 1e-9, true}, // creative: 5.0 + 4.0
		{5.0, 1 + 9.0, false},
	} {
		if got := WithinContainerRange(c.rng, c.x, 0.5, 0.5, 0, 0, 0); got != c.want {
			t.Errorf("rng=%v x=%v: got %v, want %v", c.rng, c.x, got, c.want)
		}
	}
}
