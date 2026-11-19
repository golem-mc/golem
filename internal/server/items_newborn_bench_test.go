package server

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/registry"
)

// BenchmarkTickItemsNewbornPickup times a tick with 200 newborn zero-delay
// items split among eight survival players, each in pickup range with room.
// Birth and inventory resets are untimed; pickups/op exposes whether the
// implementation attempts pickup before the item's first step.
func BenchmarkTickItemsNewbornPickup(b *testing.B) {
	s, w := newTrackerBenchServer(b)
	clients := s.clientsLocked()
	for i := range trackerBenchBodies {
		pos := clients[i%len(clients)].player.Position()
		s.spawnItem(w, protocol.ItemStack{ItemID: int32(registry.ItemDiamond), Count: 1},
			pos.X, pos.Y, pos.Z, 0, 0, 0, 0)
	}
	items := append([]*itemEntity(nil), s.items...)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		b.StopTimer()
		for _, c := range clients {
			c.player.Inventory(func(inv *player.Inventory) { *inv = player.Inventory{} })
		}
		for _, it := range items {
			it.skipStep = true
			it.item.Dead = false
			it.item.Stack.Count = 1
		}
		b.StartTimer()
		s.tickItems()
	}
	b.StopTimer()
	picked := 0
	for _, it := range items {
		if it.skipStep || it.item.Age != 0 || it.item.PickupDelay != 0 {
			b.Fatalf("item did not take the newborn branch: %+v", it)
		}
		if it.item.Dead {
			picked++
		}
	}
	b.ReportMetric(float64(picked), "pickups/op")
}
