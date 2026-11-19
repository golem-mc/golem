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

// TestAFallingBlazeKeepsSixtyPercentOfItsFall: off the ground a falling
// blaze's VY is scaled by 0.6; on the ground, or rising, it is left alone.
// vanilla: Blaze.aiStep
func TestAFallingBlazeKeepsSixtyPercentOfItsFall(t *testing.T) {
	for _, tc := range []struct {
		onGround bool
		vy, want float64
	}{{false, -1, -0.6}, {true, -1, -1}, {false, 0.2, 0.2}} {
		m := NewMob(TypeBlaze, 1, 0.5, 64, 0.5, 1)
		m.blaze.nextOffset = 50
		m.OnGround, m.VY = tc.onGround, tc.vy
		m.blazeStep(&tickView{w: newTestWorld()})
		if m.VY < tc.want-1e-9 || m.VY > tc.want+1e-9 {
			t.Errorf("onGround=%v vy=%v: got %v, want %v", tc.onGround, tc.vy, m.VY, tc.want)
		}
	}
}

// TestABlazeRetaliatesAgainstAMobThatHurtIt: the grudge names a non-player
// attacker as the target.
// vanilla: HurtByTargetGoal
func TestABlazeRetaliatesAgainstAMobThatHurtIt(t *testing.T) {
	w := newTestWorld()
	floor64(w, t)
	b := NewMob(TypeBlaze, 1, 0.5, 64, 0.5, 3)
	o := NewMob(TypeZombie, 2, 1.5, 64, 0.5, 4)
	b.retaliate = o.ID
	view := &tickView{w: w, others: []*Mob{b, o}}
	var goal *hurtByGoal
	for _, g := range blazeGoals(b) {
		if h, ok := g.(*hurtByGoal); ok {
			goal = h
		}
	}
	if goal == nil || !goal.CanStart(view) {
		t.Fatal("the blaze did not take the mob's grudge")
	}
	goal.Start(view)
	if b.TargetID != o.ID {
		t.Errorf("target = %d, want %d", b.TargetID, o.ID)
	}
	if b.targetIn(view) == nil {
		t.Error("targetIn does not resolve the mob target")
	}
}
