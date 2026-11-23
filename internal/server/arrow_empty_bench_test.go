package server

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/world"
)

// BenchmarkNoArrowsWithBoats measures the arrow pass with no arrow loaded
// and many hulls present: it must not snapshot the other entities.
func BenchmarkNoArrowsWithBoats(b *testing.B) {
	s, w := newTrackerBenchServer(b)
	for i := range 200 {
		s.spawnBoat(w, "minecraft:oak_chest_boat", float64((i%20)*4), float64(world.GroundLevel), float64(10+(i/20)*4), 0, customName{})
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		s.tickArrows()
	}
}
