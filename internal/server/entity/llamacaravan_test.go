package entity

import (
	"math/rand"
	"testing"
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
