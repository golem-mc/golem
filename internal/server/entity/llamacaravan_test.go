package entity

import (
	"math/rand"
	"testing"

	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

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

// TestCaravanGridMatchesHerdScan checks that the grid query picks the llama
// to follow that the whole-herd scan does, over random herds with leashed
// leads and caravans at the edge of the reach.
func TestCaravanGridMatchesHerdScan(t *testing.T) {
	w := newTestWorld()
	rng := rand.New(rand.NewSource(34))
	joins := 0
	for round := range 30 {
		herd := make([]*Mob, 0, 120)
		for i := range cap(herd) {
			x := float64(rng.Intn(41)-20) + rng.Float64()*float64(rng.Intn(2))
			z := float64(rng.Intn(41)-20) + rng.Float64()*float64(rng.Intn(2))
			y := 64 + float64(rng.Intn(13)-6)
			kind := TypeLlama
			if i%7 == 0 {
				kind = TypeCow
			}
			m := NewMob(kind, int32(i+1), x, y, z, int64(i))
			if kind == TypeLlama && rng.Intn(8) == 0 {
				m.LeashHolderID = 999
			}
			herd = append(herd, m)
		}
		// Chain a few llamas behind leashed ones, so some candidates are
		// caravan tails and some are already followed.
		for i, m := range herd[1:] {
			ahead := herd[i]
			if IsLlama(m.Type) && IsLlama(ahead.Type) && (ahead.leashed() || ahead.inCaravan()) && ahead.Horse.tail == nil && rng.Intn(2) == 0 {
				m.joinCaravan(ahead)
			}
		}
		grid := takePushGrid(len(herd), nil)
		grid.build(herd)
		tv := &tickView{w: w, others: herd}
		for _, m := range herd {
			if !IsLlama(m.Type) || m.leashed() || m.inCaravan() {
				continue
			}
			g := &caravanGoal{m: m, speed: llamaCaravanSpeed}
			m.push = nil
			ok := g.CanStart(tv)
			head := m.Horse.head
			m.leaveCaravan()
			m.push = grid
			gok := g.CanStart(tv)
			ghead := m.Horse.head
			m.leaveCaravan()
			m.push = nil
			if ok != gok || head != ghead {
				t.Fatalf("round %d, llama %d: scan (%v %v), grid (%v %v)", round, m.ID, ok, head, gok, ghead)
			}
			if ok {
				joins++
			}
		}
		grid.release()
	}
	if joins < 100 {
		t.Errorf("only %d llamas joined a caravan; the herds test too little", joins)
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

// TestCaravanSearchStaysInTheEntitySectionWindow: a scaled lead whose box
// reaches the search window but whose position is filed in a section the
// window misses is not joined, in the herd scan and the grid alike.
// vanilla: LlamaFollowCaravanGoal.canUse, EntitySectionStorage.forEachAccessibleNonEmptySection
func TestCaravanSearchStaysInTheEntitySectionWindow(t *testing.T) {
	w := newTestWorld()
	for _, grid := range []bool{false, true} {
		herd := make([]*Mob, 0, 70)
		lead := NewMob(TypeLlama, 1, 160.5, 64, 159.5, 1)
		lead.LeashHolderID = 99
		lead.SetAttributes(lead.BuildAttributes(nbt.List{}))
		lead.attrs.SetBase(registry.AttributeScale, 12)
		follower := NewMob(TypeLlama, 2, 146.5, 64, 159.5, 2)
		herd = append(herd, lead, follower)
		for i := range 64 {
			// The grid case fills the candidate list past the grid's
			// threshold with leashed llamas far from the follower.
			if grid {
				far := NewMob(TypeLlama, int32(10+i), 1000.5+float64(i)*20, 64, 0.5, int64(i))
				far.LeashHolderID = 99
				herd = append(herd, far)
				continue
			}
			herd = append(herd, NewMob(TypeCow, int32(10+i), 0.5+float64(i), 64, 0.5, int64(i)))
		}
		view := &tickView{w: w, others: herd}
		if grid {
			g := takePushGrid(len(herd), nil)
			g.build(herd)
			for _, m := range herd {
				m.push = g
			}
			defer g.release()
		}
		if (&caravanGoal{m: follower, speed: llamaCaravanSpeed}).CanStart(view) {
			t.Errorf("grid=%v: the llama joined a lead filed outside the search sections", grid)
		}
		follower.X = 150.5
		if !(&caravanGoal{m: follower, speed: llamaCaravanSpeed}).CanStart(view) {
			t.Errorf("grid=%v: the llama did not join a lead in the search sections", grid)
		}
	}
}

// TestCaravanSearchReachesTheSectionBelow: the window reaches 4 blocks below
// the query box, so a tall lead filed in the section under it is found.
// vanilla: EntitySectionStorage.forEachAccessibleNonEmptySection
func TestCaravanSearchReachesTheSectionBelow(t *testing.T) {
	w := newTestWorld()
	lead := NewMob(TypeLlama, 1, 156.5, 63, 159.5, 1)
	lead.LeashHolderID = 99
	lead.SetAttributes(lead.BuildAttributes(nbt.List{}))
	lead.attrs.SetBase(registry.AttributeScale, 4)
	follower := NewMob(TypeLlama, 2, 150.5, 70, 159.5, 2)
	view := &tickView{w: w, others: []*Mob{lead, follower}}
	if !(&caravanGoal{m: follower, speed: llamaCaravanSpeed}).CanStart(view) {
		t.Error("the llama missed a lead filed in the section below the window")
	}
}

// TestCaravanGridFindsAScaledLeadAcrossCells: the grid's reach counts the
// scale attribute, so a scale-12 lead whose box overlaps the window is found
// with the list forced onto the grid.
// vanilla: LlamaFollowCaravanGoal.canUse
func TestCaravanGridFindsAScaledLeadAcrossCells(t *testing.T) {
	lead := NewMob(TypeLlama, 1, 164.1, 64, 159.5, 1)
	lead.LeashHolderID = 99
	lead.SetAttributes(lead.BuildAttributes(nbt.List{}))
	lead.attrs.SetBase(registry.AttributeScale, 12)
	follower := NewMob(TypeLlama, 2, 149.9, 64, 159.5, 2)
	herd := []*Mob{lead, follower}
	for i := range 64 {
		far := NewMob(TypeLlama, int32(10+i), 1000.5+float64(i)*20, 64, 0.5, int64(i))
		far.LeashHolderID = 99
		herd = append(herd, far)
	}
	g := takePushGrid(len(herd), nil)
	defer g.release()
	g.build(herd)
	for _, m := range herd {
		m.push = g
	}
	view := &tickView{w: newTestWorld(), others: herd}
	if !(&caravanGoal{m: follower, speed: llamaCaravanSpeed}).CanStart(view) || follower.Horse.head != lead {
		t.Error("the grid missed the scale-12 lead overlapping the search window")
	}
}
