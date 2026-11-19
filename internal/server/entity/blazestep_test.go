package entity

import "testing"

// TestABlazeRisesTowardATargetAboveItsHeightOffset: a target whose eyes are
// above the blaze's eyes plus the allowed offset lifts the blaze toward 0.3
// a tick; a target below it does not.
// vanilla: Blaze.customServerAiStep
func TestABlazeRisesTowardATargetAboveItsHeightOffset(t *testing.T) {
	for _, tc := range []struct {
		name    string
		targetY float64
		rises   bool
	}{{"above", 70, true}, {"level", 64, false}} {
		w := newTestWorld()
		m := NewMob(TypeBlaze, 1, 0.5, 64, 0.5, 1)
		m.blaze.offset = 0.5
		m.blaze.nextOffset = 50
		m.TargetID = 9
		view := &tickView{w: w, players: []Player{{EntityID: 9, X: 0.5, Y: tc.targetY, Z: 5.5, Huntable: true}}}
		m.blazeStep(view)
		if tc.rises {
			if want := 0.3 * 0.3; m.VY < want-1e-6 || m.VY > want+1e-6 {
				t.Errorf("%s: VY = %v, want %v", tc.name, m.VY, want)
			}
		} else if m.VY != 0 {
			t.Errorf("%s: VY = %v, want 0", tc.name, m.VY)
		}
	}
}
