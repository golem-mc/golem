package entity

import "testing"

// TestTheDrawnHomeCheckMeasuresToTheHomeBlocksCentre: the Vec3 overload of
// vanilla's home check measures from the centre of the home block to the
// drawn position, not between the cells the two fall in, so a draw near the
// radius edge is kept or dropped by where it is, not where its cell is.
// vanilla: Mob.isWithinHome(Vec3), Vec3i.distToCenterSqr
func TestTheDrawnHomeCheckMeasuresToTheHomeBlocksCentre(t *testing.T) {
	m := NewMob(TypeGhast, 1, 0.5, 64.5, 0.5, 1)
	// A home at block (0, 64, 0), centre (0.5, 64.5, 0.5), radius 3.
	m.RestrictTo(0, 64, 0, 3)
	for _, tc := range []struct {
		name    string
		x, y, z float64
		within  bool
	}{
		// One cell out along x but still 2.65 from the centre: the cell
		// rule dropped this draw; the position's own distance keeps it.
		{"a near draw in the next cell", 3.2, 64.64, 0.44, true},
		// The far corner of a cell whose low corner is inside the radius:
		// the cell rule kept it; its position is past the radius.
		{"a far draw inside the radius's cells", 2.96, 66.96, 0.96, false},
		// Exactly the radius away: the check is strictly under it.
		{"a draw exactly the radius out", 3.5, 64.5, 0.5, false},
		// Well inside, for the shape of the ball.
		{"a draw near the centre", 0.5, 64.4, 0.6, true},
	} {
		if got := m.withinHomeAt(tc.x, tc.y, tc.z); got != tc.within {
			t.Errorf("%s: withinHomeAt(%v, %v, %v) = %t, want %t",
				tc.name, tc.x, tc.y, tc.z, got, tc.within)
		}
	}
	// No home set: every draw is kept, as the radius -1 means.
	m.home = homeState{}
	if !m.withinHomeAt(1000, 100, 1000) {
		t.Error("a home-less mob dropped a draw, and there is no home to drop it from")
	}
}
