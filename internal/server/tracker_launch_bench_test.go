package server

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
	"github.com/google/uuid"
)

// BenchmarkLaunchFireworkWatched times a rocket's launch, through the spawn
// packets queued to the players in range of it. The rockets are cleared every
// 256 launches, outside the timed section.
func BenchmarkLaunchFireworkWatched(b *testing.B) {
	s, w := newTrackerBenchServer(b)
	stack := protocol.ItemStack{ItemID: int32(registry.ItemFireworkRocket), Count: 1}
	x, z := trackerBenchSpot(0)
	reset := func() {
		for _, it := range append([]*fireworkEntity(nil), s.fireworks...) {
			s.killFirework(it)
		}
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := range b.N {
		if i%256 == 255 {
			b.StopTimer()
			reset()
			b.StartTimer()
		}
		s.spawnFirework(w, stack, x, float64(world.GroundLevel)+5, z, 0, uuid.Nil, 0)
	}
}
