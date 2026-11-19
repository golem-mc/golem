package entity

import (
	"math"
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

// TestAFloatTargetAboveOpenSkyIsNotMirrored: a column with no terrain
// holds no mirror, so draws rise above the body.
func TestAFloatTargetAboveOpenSkyIsNotMirrored(t *testing.T) {
	w := motionWorld{motion: -65}
	var above int
	for seed := range int64(200) {
		m := NewMob(TypeGhast, 1, 0, 64, 0, seed)
		m.RestrictTo(1000, 64, 1000, 1)
		if _, y, _ := m.flyTarget(w, true); y > m.Y {
			above++
		}
	}
	if above == 0 {
		t.Error("no float target rose above the body over open sky")
	}
}

// TestAHomeKeptFlyTargetLiesInsideTheHomeBlocksCentre: a kept draw lies
// strictly within the home's reach of the home block's centre, the measure
// vanilla takes from a drawn position — not inside the cells the radius
// spans, which lets a draw near the edge land past the radius.
// vanilla: Ghast.RandomFloatAroundGoal.chooseRandomPositionWithRestriction, Mob.isWithinHome(Vec3)
func TestAHomeKeptFlyTargetLiesInsideTheHomeBlocksCentre(t *testing.T) {
	w := motionWorld{motion: -65}
	var escaped int
	for seed := range int64(400) {
		m := NewMob(TypeGhast, 1, 0.5, 64.5, 0.5, seed)
		m.RestrictTo(0, 64, 0, 16)
		x, y, z := m.flyTarget(w, true)
		dx, dy, dz := x-0.5, y-64.5, z-0.5
		if dx*dx+dy*dy+dz*dz >= 16*16 {
			escaped++
			if escaped == 1 {
				t.Errorf("seed %d: fly target (%v, %v, %v) lies %v from the home's centre, past its radius 16",
					seed, x, y, z, math.Sqrt(dx*dx+dy*dy+dz*dz))
			}
		}
	}
	if escaped > 0 {
		t.Errorf("%d of 400 seeds drew a target past the home's radius", escaped)
	}
}
