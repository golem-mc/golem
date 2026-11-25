package entity

import (
	"fmt"
	"testing"
)

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

// BenchmarkFollowParentShouldContinue measures an active follow goal's
// per-tick check with the parent last in a herd of the given size.
func BenchmarkFollowParentShouldContinue(b *testing.B) {
	for _, n := range []int{8, 64, 512, 4096} {
		b.Run(fmt.Sprintf("herd=%d", n), func(b *testing.B) {
			calf := NewMob(TypeCow, 1, 0.5, 64, 0.5, 1)
			calf.BabyAge = -24000
			others := []*Mob{calf}
			byID := map[int32]*Mob{1: calf}
			for i := 0; i < n-2; i++ {
				filler := NewMob(TypeCow, int32(100+i), 40.5, 64, 40.5, int64(100+i))
				others = append(others, filler)
				byID[filler.ID] = filler
			}
			parent := NewMob(TypeCow, 2, 8.5, 64, 0.5, 2)
			others = append(others, parent)
			byID[2] = parent
			view := &tickView{w: testWorld{}, others: others, byID: byID}
			g := &followParentGoal{m: calf, parent: parent}
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				if !g.ShouldContinue(view) {
					b.Fatal("dropped the parent")
				}
			}
		})
	}
}

// TestFollowParentFindsRiderSeatedAcrossCell: a grown cow carried into
// another push cell by its vehicle is found by a calf at the edge of the
// search, as the whole-herd scan finds it.
func TestFollowParentFindsRiderSeatedAcrossCell(t *testing.T) {
	calf := NewMob(TypeCow, 1, 8.85, 64, 0.5, 1)
	calf.BabyAge = -24000
	pig := NewMob(TypePig, 2, 0.3, 64, 0.5, 2)
	cow := NewMob(TypeCow, 3, -0.1, 64, 0.5, 3)
	herd := []*Mob{calf, pig, cow}
	for i := range villagerGridMinHerd {
		id := int32(10 + i)
		herd = append(herd, NewMob(TypeBat, id, 60+float64(i), 64, 60, int64(id)))
	}
	grid := takePushGrid(len(herd), nil)
	defer grid.release()
	grid.build(herd)
	Ride(pig, cow)
	pig.seatPassenger(cow)
	grid.moved(cow)
	calf.push = grid
	view := &tickView{w: testWorld{}, others: herd}
	g := &followParentGoal{m: calf}
	if !g.CanStart(view) || g.parent != cow {
		t.Fatalf("calf chose %v, want the carried cow", g.parent)
	}
}
