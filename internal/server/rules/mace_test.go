package rules

import (
	"math"
	"testing"
)

func TestSmashDamageBonus(t *testing.T) {
	for _, tc := range []struct{ fall, perBlock, want float64 }{
		{2, 0, 8},
		{3, 0, 12},
		{5, 0, 16},
		{8, 0, 22},
		{10, 0, 24},
		{10, 0.5, 29},
	} {
		if got := SmashDamageBonus(tc.fall, tc.perBlock); math.Abs(got-tc.want) > 1e-9 {
			t.Errorf("SmashDamageBonus(%v, %v) = %v, want %v", tc.fall, tc.perBlock, got, tc.want)
		}
	}
}

func TestSmashKnockback(t *testing.T) {
	// One block off a light smash, then a heavy one, then a body that
	// resists half.
	for _, tc := range []struct{ fall, resist, wantX float64 }{
		{4, 0, 2.5 * smashKnockbackPower},
		{6, 0, 5 * smashKnockbackPower},
		{6, 0.5, 2.5 * smashKnockbackPower},
	} {
		vx, vy, vz, ok := SmashKnockback(1, 0, 0, tc.fall, tc.resist)
		if !ok || math.Abs(vx-tc.wantX) > 1e-9 || vy != SmashLift || vz != 0 {
			t.Errorf("SmashKnockback(fall %v, resist %v) = (%v, %v, %v, %v), want (%v, %v, 0, true)",
				tc.fall, tc.resist, vx, vy, vz, ok, tc.wantX, SmashLift)
		}
	}
	if _, _, _, ok := SmashKnockback(3.6, 0, 0, 6, 0); ok {
		t.Error("a body past the radius was pushed")
	}
	if _, _, _, ok := SmashKnockback(1, 0, 0, 6, 1); ok {
		t.Error("a body resisting all knockback was pushed")
	}
}
