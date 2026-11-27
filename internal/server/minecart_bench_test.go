package server

import (
	"fmt"
	"testing"

	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/world"
)

// BenchmarkTickMinecartsInHerd times one empty cart, parked or rolling, far
// from a herd of cows.
func BenchmarkTickMinecartsInHerd(b *testing.B) {
	for _, moving := range []bool{false, true} {
		for _, cows := range []int{0, 2000} {
			name := fmt.Sprintf("parked/%d", cows)
			if moving {
				name = fmt.Sprintf("moving/%d", cows)
			}
			b.Run(name, func(b *testing.B) {
				s, w := newTrackerBenchServer(b)
				dim := s.allDims()[0]
				for i := range cows {
					s.spawnMobInDim(dim, entity.TypeCow, float64(i%45)-22, float64(world.GroundLevel), float64(i/45)-22, int64(i+1), nil)
				}
				cart := s.spawnMinecart(w, entity.MinecartPlain, 40, float64(world.GroundLevel), 40)
				step := func() {
					if moving {
						cart.cart.X, cart.cart.Z, cart.cart.VX = 40, 40, 0.3
					}
					s.tickMinecarts()
				}
				for range 20 {
					step()
				}
				b.ReportAllocs()
				b.ResetTimer()
				for range b.N {
					step()
				}
			})
		}
	}
}
