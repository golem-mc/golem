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

// TestLlamaCaravanSearchInALargeHerdMatchesTheWholeHerd: past the size at
// which the search asks the push grid, each llama picks the same head as a
// scan of the whole herd would.
func TestLlamaCaravanSearchInALargeHerdMatchesTheWholeHerd(t *testing.T) {
	const herd = 200
	build := func() []*Mob {
		mobs := make([]*Mob, herd)
		for i := range mobs {
			// A 20x10 lattice 3 blocks apart, the leashed leads in one column.
			mobs[i] = NewMob(TypeLlama, int32(i+1), float64(i%20)*3+0.5, 64+float64(i%3), float64(i/20)*3+0.5, int64(i))
			if i%20 == 0 {
				mobs[i].LeashHolderID = 999
			}
		}
		return mobs
	}
	heads := func(grid bool) []int32 {
		mobs := build()
		g := takePushGrid(herd, nil)
		defer g.release()
		g.build(mobs)
		view := &tickView{w: newTestWorld(), others: mobs}
		out := make([]int32, herd)
		for i, m := range mobs {
			if grid {
				m.push = g
			}
			(&caravanGoal{m: m, speed: llamaCaravanSpeed}).CanStart(view)
			m.push = nil
			if m.Horse.head != nil {
				out[i] = m.Horse.head.ID
			}
		}
		return out
	}
	want, got := heads(false), heads(true)
	joined := 0
	for i := range want {
		if want[i] != got[i] {
			t.Fatalf("llama %d follows %d through the grid, %d in a whole-herd scan", i+1, got[i], want[i])
		}
		if want[i] != 0 {
			joined++
		}
	}
	if joined == 0 {
		t.Fatal("no llama joined a caravan")
	}
}
