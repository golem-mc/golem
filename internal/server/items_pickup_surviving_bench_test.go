package server

import (
	"strconv"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/registry"
)

// BenchmarkSurvivingClusteredPickup times a pickup pass over one drop that n
// players with full inventories all touch: the drop survives every touch, so
// the pass hands it from each player to the next.
func BenchmarkSurvivingClusteredPickup(b *testing.B) {
	for _, n := range []int{20, 100} {
		b.Run(strconv.Itoa(n), func(b *testing.B) {
			s, w := newTrackerBenchServer(b)
			s.mu.Lock()
			clear(s.clients)
			s.mu.Unlock()
			dim := s.allDims()[0]
			full := protocol.ItemStack{ItemID: int32(registry.ItemDirt), Count: 64}
			for i := range n {
				c := addPickupBenchClient(s, dim, 15.8+float64(i%4)*0.1, 3)
				c.player.Inventory(func(inv *player.Inventory) {
					for slot := range inv.Slots {
						inv.Set(slot, full)
					}
				})
			}
			stack := protocol.ItemStack{ItemID: int32(registry.ItemStone), Count: 1}
			it := s.spawnItem(w, stack, 16, 64, 3, 0, 0, 0, 0)
			touches := 0
			s.pickupSeam = func(int32) { touches++ }
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				s.tickItemPickups()
			}
			b.StopTimer()
			if it.item.Dead || touches != n*b.N {
				b.Fatalf("drop dead %t after %d touches, want alive after %d", it.item.Dead, touches, n*b.N)
			}
		})
	}
}
