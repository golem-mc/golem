package server

import (
	"fmt"
	"testing"

	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/world"
)

// BenchmarkTickBoatsInHerd times one empty boat far from a herd of cows.
func BenchmarkTickBoatsInHerd(b *testing.B) {
	for _, cows := range []int{0, 2000} {
		b.Run(fmt.Sprint(cows), func(b *testing.B) {
			s, w := newTrackerBenchServer(b)
			dim := s.allDims()[0]
			for i := range cows {
				s.spawnMobInDim(dim, entity.TypeCow, float64(i%45)-22, float64(world.GroundLevel), float64(i/45)-22, int64(i+1), nil)
			}
			s.spawnBoat(w, "minecraft:oak_boat", 40, float64(world.GroundLevel), 40, 0, customName{})
			for range 20 {
				s.tickBoats()
			}
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				s.tickBoats()
			}
		})
	}
}
