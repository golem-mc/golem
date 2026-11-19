package rules

import (
	"math"
	"testing"
)

func TestHoglinThrowPushesAwayAndUp(t *testing.T) {
	vx, vy, vz, ok := HoglinThrow(0, 3, 0, 0, 0.5, 0.25)
	if !ok || math.Abs(vx-0.5) > 1e-9 || math.Abs(vy-0.25) > 1e-9 || math.Abs(vz) > 1e-9 {
		t.Errorf("got %v %v %v %v, want 0.5 0.25 0 true", vx, vy, vz, ok)
	}
}
