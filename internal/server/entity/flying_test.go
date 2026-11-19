package entity

import (
	"testing"
)

// TestAFloatTargetOverTerrainIsMirroredBelowTheBody: when every draw falls
// outside the home, the fresh point above the terrain's height is mirrored to
// the other side of the body's height, so it never lies above the body.
// vanilla: Ghast.RandomFloatAroundGoal.getSuitableFlyToPosition
func TestAFloatTargetOverTerrainIsMirroredBelowTheBody(t *testing.T) {
	w := motionWorld{motion: 60}
	for seed := range int64(200) {
		m := NewMob(TypeGhast, 1, 0, 64, 0, seed)
		m.RestrictTo(1000, 64, 1000, 1)
		if _, y, _ := m.flyTarget(w, true); y > m.Y {
			t.Fatalf("seed %d: float target at y=%v lies above the body at %v over terrain at 61", seed, y, m.Y)
		}
	}
}
