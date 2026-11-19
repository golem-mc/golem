package entity

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/registry"
)

// BenchmarkMobsTickFlyersAndGatherers steps bees, parrots and villagers with
// bread on the ground: the flight control, the parrot's air wander, the
// wanted-item scan and the inventory room the allay shares with them.
func BenchmarkMobsTickFlyersAndGatherers(b *testing.B) {
	w := newTestWorld()
	floor(w, -65, -65, 64, 64, 63, defaultState(b, "minecraft:stone"))
	bread := protocol.ItemStack{ItemID: int32(registry.ItemBread), Count: 1}
	kinds := [...]MobType{TypeBee, TypeParrot, TypeVillager}
	mobs := make(Mobs, 0, 90)
	for i := range 90 {
		y := 66.0
		if kinds[i%3] == TypeVillager {
			y = 64
		}
		m := NewMob(kinds[i%3], int32(i+1), float64(i%10)*6-30, y, float64(i/10)*6-30, int64(i+1))
		m.CanPickUpLoot = true
		mobs = append(mobs, m)
	}
	items := make([]LootItem, 0, 20)
	for i := range 20 {
		items = append(items, LootItem{X: float64(i)*3 - 30, Y: 64, Z: 20, Stack: bread})
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		mobs.SenseWantedItems(w, items)
		mobs.Tick(w, nil, nil)
	}
}
