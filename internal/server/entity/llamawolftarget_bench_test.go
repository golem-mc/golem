package entity

import (
	"math"
	"math/rand"
	"testing"
)

func llamaWolfHerd(n int, wolf bool) []*Mob {
	rng := rand.New(rand.NewSource(33))
	mobs := make([]*Mob, n)
	for i := range mobs {
		mobs[i] = NewMob(TypeLlama, int32(i+1), (rng.Float64()-0.5)*60, 64, (rng.Float64()-0.5)*60, int64(i))
	}
	if wolf {
		mobs[0] = NewMob(TypeWolf, 1, 0, 64, 0, 1)
	}
	return mobs
}

func llamaWolfGoal(m *Mob) *mobTargetGoal {
	return &mobTargetGoal{m: m, victim: TypeWolf, victimOnly: true, unsighted: true, eager: true,
		followShare: llamaWolfFollow, matches: isWildWolf}
}

// BenchmarkLlamaWolfTarget runs every llama's wolf target scan in a herd of
// 2,000 holding no wolf.
func BenchmarkLlamaWolfTarget(b *testing.B) {
	mobs := llamaWolfHerd(2000, false)
	grid := takePushGrid(len(mobs), nil)
	defer grid.release()
	grid.build(mobs)
	view := &tickView{w: newTestWorld(), others: mobs}
	goals := make([]*mobTargetGoal, len(mobs))
	for i, m := range mobs {
		goals[i] = llamaWolfGoal(m)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		for i, m := range mobs {
			m.push = grid
			if goals[i].CanStart(view) {
				b.Fatal("a llama targeted a wolf in a herd without one")
			}
			m.push = nil
		}
	}
}

func TestLlamaWolfTargetSeesAWolfInTheHerd(t *testing.T) {
	mobs := llamaWolfHerd(20, true)
	mobs[1].X, mobs[1].Z = 1, 1
	grid := takePushGrid(len(mobs), nil)
	defer grid.release()
	grid.build(mobs)
	llama := mobs[1]
	llama.push = grid
	defer func() { llama.push = nil }()
	g := llamaWolfGoal(llama)
	if math.IsNaN(g.follow()) || !g.CanStart(&tickView{w: newTestWorld(), others: mobs}) || llama.TargetID != mobs[0].ID {
		t.Fatal("a llama beside a wild wolf did not target it")
	}
}
