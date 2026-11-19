package server

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// BenchmarkTickRedstoneTouchItems measures the touch pass over 2000 settled
// items on ground without touch blocks.
func BenchmarkTickRedstoneTouchItems(b *testing.B) {
	s, w := newTrackerBenchServer(b)
	for i := range 2000 {
		it := s.spawnItem(w, protocol.ItemStack{ItemID: int32(registry.ItemStone), Count: 1},
			float64(i%40)-20+0.5, float64(world.GroundLevel), float64(i/40)-25+0.5,
			0, 0, 0, -1)
		// Keep all 2000 bodies: items pushed out of the ground may meet while settling.
		it.item.Age = entity.ItemUnlimitedAge
	}
	for range 30 {
		s.tickItems()
	}
	if n := len(s.items); n != 2000 {
		b.Fatalf("%d items after settling, want 2000", n)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		s.tickRedstoneTouch()
	}
}
