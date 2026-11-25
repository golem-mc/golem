package entity

import "testing"

// TestMoveWithoutVerticalMotionClearsGround verifies that a grounded mob
// moving sideways with zero vertical motion is airborne after the move, and
// that a spawn's first tick keeps the flag.
// vanilla: Entity.move setOnGroundWithMovement
func TestMoveWithoutVerticalMotionClearsGround(t *testing.T) {
	for _, tc := range []struct {
		age  int
		want bool
	}{{1, true}, {2, false}} {
		w := flatWorld(t)
		m := NewMob(TypePig, 1, 0.5, 64, 0.5, 1)
		m.OnGround, m.GroundGuess = true, true
		m.Age = tc.age
		m.move(w, 0.45, 0, 0)
		if m.OnGround != tc.want {
			t.Errorf("age %d: OnGround = %v after a level move, want %v", tc.age, m.OnGround, tc.want)
		}
		m.move(w, 0.45, -0.0784, 0)
		if !m.OnGround {
			t.Errorf("age %d: a falling move onto stone left the mob airborne", tc.age)
		}
	}
}
