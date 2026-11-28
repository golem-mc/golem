package rules

import (
	"math"
	"testing"
)

func TestHoglinThrowPushesAwayAndUp(t *testing.T) {
	vx, vy, vz, ok := HoglinThrow(1, 0, 3, 0, 0, 0.5, 0.25)
	if !ok || math.Abs(vx-0.5) > 1e-9 || math.Abs(vy-0.25) > 1e-9 || math.Abs(vz) > 1e-9 {
		t.Errorf("got %v %v %v %v, want 0.5 0.25 0 true", vx, vy, vz, ok)
	}
}

func TestHoglinThrowScalesWithAttackKnockback(t *testing.T) {
	_, vy1, _, _ := HoglinThrow(1, 0, 3, 0, 0, 0.5, 0.25)
	_, vy3, _, ok := HoglinThrow(3, 0, 3, 0, 0, 0.5, 0.25)
	if !ok || vy3 != 3*vy1 {
		t.Errorf("vy = %v, want %v", vy3, 3*vy1)
	}
	if _, _, _, ok := HoglinThrow(0, 0, 3, 0, 0, 0.5, 0.25); ok {
		t.Error("zero knockback still throws")
	}
}
