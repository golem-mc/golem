package entity

import "testing"

// TestLlamaJoinsALeashedCaravanAndLeavesWhenItBreaks: a llama falls in behind
// a leashed llama, a third behind the second, and a lost lead lets go.
// vanilla: LlamaFollowCaravanGoal.canUse, canContinueToUse
func TestLlamaJoinsALeashedCaravanAndLeavesWhenItBreaks(t *testing.T) {
	w := newTestWorld()
	lead := NewMob(TypeLlama, 1, 0.5, 64, 0.5, 1)
	lead.LeashHolderID = 99
	second := NewMob(TypeLlama, 2, 0.5, 64, 4.5, 2)
	third := NewMob(TypeTraderLlama, 3, 0.5, 64, 12.5, 3)
	view := &tickView{w: w, others: []*Mob{lead, second, third}}

	g2 := &caravanGoal{m: second, speed: llamaCaravanSpeed}
	g3 := &caravanGoal{m: third, speed: llamaCaravanSpeed}
	if g3.CanStart(view) {
		t.Fatal("a llama joined a caravan with no leashed lead")
	}
	if !g2.CanStart(view) || second.Horse.head != lead || lead.Horse.tail != second {
		t.Fatal("the second llama did not follow the leashed one")
	}
	if !g3.CanStart(view) || third.Horse.head != second {
		t.Fatal("the third llama did not follow the second")
	}
	if !g2.ShouldContinue(view) || !g3.ShouldContinue(view) {
		t.Fatal("caravan does not continue with a leashed lead")
	}
	lead.LeashHolderID = 0
	if g2.ShouldContinue(view) || g3.ShouldContinue(view) {
		t.Fatal("caravan continues without a leashed lead")
	}
	g2.Stop(view)
	if second.inCaravan() || lead.Horse.tail != nil || g2.speed != llamaCaravanSpeed {
		t.Error("stop did not leave the caravan")
	}
}

// TestLlamaCaravanTrailsAtTwoBlocks: the llama aims at the point two blocks
// short of the one ahead.
// vanilla: LlamaFollowCaravanGoal.tick
func TestLlamaCaravanTrailsAtTwoBlocks(t *testing.T) {
	w := newTestWorld()
	floor64(w, t)
	lead := NewMob(TypeLlama, 1, 0.5, 64, -3.5, 1)
	lead.LeashHolderID = 99
	tail := NewMob(TypeLlama, 2, 0.5, 64, 3.5, 2)
	view := &tickView{w: w, others: []*Mob{lead, tail}}
	g := &caravanGoal{m: tail, speed: llamaCaravanSpeed}
	if !g.CanStart(view) {
		t.Fatal("did not join")
	}
	g.Tick(view)
	if tail.navDone() {
		t.Error("the trailing llama has no path")
	}
}

// TestLlamaCaravanHoldsStillOnAKnot: a llama whose lead is on a fence knot
// makes no new path.
// vanilla: LlamaFollowCaravanGoal.tick
func TestLlamaCaravanHoldsStillOnAKnot(t *testing.T) {
	w := newTestWorld()
	floor64(w, t)
	lead := NewMob(TypeLlama, 1, 0.5, 64, -3.5, 1)
	lead.LeashHolderID = 99
	tail := NewMob(TypeLlama, 2, 0.5, 64, 3.5, 2)
	view := &tickView{w: w, others: []*Mob{lead, tail}}
	g := &caravanGoal{m: tail, speed: llamaCaravanSpeed}
	if !g.CanStart(view) {
		t.Fatal("did not join")
	}
	tail.Horse.knotHeld = true
	g.Tick(view)
	if !tail.navDone() {
		t.Error("a llama on a knot made a path to the caravan")
	}
}
