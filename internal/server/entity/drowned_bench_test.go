package entity

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/registry"
)

// BenchmarkDrownedHunt exercises the ranged and melee goal selection and beach
// searches for sixteen drowned with a player on a stone floor.
func BenchmarkDrownedHunt(b *testing.B) {
	w := newTestWorld()
	witchFloor(w, b)
	var mobs []*Mob
	for i := range 16 {
		m := NewMob(TypeDrowned, int32(i+1), float64(i%4*4)-8, 64, float64(i/4*4)-8, int64(i))
		m.Persistence = true
		if i%2 == 0 {
			m.MainHand = protocol.ItemStack{ItemID: int32(registry.ItemTrident), Count: 1}
		}
		mobs = append(mobs, m)
	}
	players := witchPrey(8)
	for b.Loop() {
		for _, m := range mobs {
			m.tick(w, players, mobs)
		}
	}
}
