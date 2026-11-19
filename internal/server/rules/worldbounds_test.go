package rules

import (
	"math"
	"testing"
)

// TestInWorldBoundsHorizontal checks the edges of the world's extent: the
// lower bound is inside, the upper one outside, on either axis.
func TestInWorldBoundsHorizontal(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		x, z int32
		want bool
	}{
		{0, 0, true},
		{-30000000, -30000000, true},
		{29999999, 29999999, true},
		{30000000, 0, false},
		{0, 30000000, false},
		{-30000001, 0, false},
		{0, -30000001, false},
		{math.MaxInt32, math.MaxInt32, false},
		{math.MinInt32, math.MinInt32, false},
	} {
		if got := InWorldBoundsHorizontal(tc.x, tc.z); got != tc.want {
			t.Errorf("InWorldBoundsHorizontal(%d, %d) = %v, want %v", tc.x, tc.z, got, tc.want)
		}
	}
}
