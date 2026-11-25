package entity

import "testing"

// TestFollowParentLeavesHerd keeps a parent present in the tick's herd and
// drops one that left it, with and without the tick's by-ID index.
func TestFollowParentLeavesHerd(t *testing.T) {
	for _, indexed := range []bool{false, true} {
		calf := NewMob(TypeCow, 1, 0.5, 64, 0.5, 1)
		calf.BabyAge = -24000
		parent := NewMob(TypeCow, 2, 8.5, 64, 0.5, 2)
		view := &tickView{w: testWorld{}, others: []*Mob{calf, parent}}
		if indexed {
			view.byID = map[int32]*Mob{1: calf, 2: parent}
		}
		g := &followParentGoal{m: calf}
		if !g.CanStart(view) {
			t.Fatalf("indexed=%v: calf did not select its parent", indexed)
		}
		if !g.ShouldContinue(view) {
			t.Errorf("indexed=%v: dropped a parent still in the herd", indexed)
		}
		view.others = []*Mob{calf}
		if indexed {
			view.byID = map[int32]*Mob{1: calf}
		}
		if g.ShouldContinue(view) {
			t.Errorf("indexed=%v: kept a parent that left the herd", indexed)
		}
	}
}
