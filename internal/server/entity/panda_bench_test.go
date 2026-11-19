package entity

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/registry"
)

func BenchmarkPandaEatingSound(b *testing.B) {
	m := NewMob(TypePanda, 1, 0.5, 64, 0.5, 1)
	m.MainHand = protocol.ItemStack{ItemID: int32(registry.ItemBamboo), Count: 1}
	m.pandaSetFlag(pandaFlagSit, true)
	view := &tickView{}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		m.SetMetadataPandaEat(5)
		view.events = view.events[:0]
		m.pandaEat(view)
	}
}

// BenchmarkPandaBreedGoal measures the bamboo search a panda's breeding
// attempt runs when none is near, the full 3x15x15 spiral.
func BenchmarkPandaBreedGoal(b *testing.B) {
	w := newTestWorld()
	floor(w, -10, -10, 10, 10, 63, defaultState(b, "minecraft:stone"))
	m := NewMob(TypePanda, 1, 0.5, 64, 0.5, 1)
	view := &tickView{w: w, others: []*Mob{m}}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		m.pandaFindsBamboo(view.w)
		m.pandaTick(view)
	}
}
