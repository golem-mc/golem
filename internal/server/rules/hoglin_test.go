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

func TestHoglinThrowIsCancelledByFullKnockbackResistance(t *testing.T) {
	if _, _, _, ok := HoglinThrow(1, 3, 0, 0, 0.5, 0.25); ok {
		t.Error("a target with 1.0 knockback resistance was thrown")
	}
	if _, vy, _, _ := HoglinThrow(0.6, 3, 0, 0, 0.5, 0.25); math.Abs(vy-0.1) > 1e-9 {
		t.Errorf("vertical %v, want 0.4 * 0.25 = 0.1", vy)
	}
}

func TestHoglinBlowDamageIsHalfTheAttackPlusTheRoll(t *testing.T) {
	if got := HoglinBlowDamage(6, 5); got != 8 {
		t.Errorf("got %v, want 8", got)
	}
}
