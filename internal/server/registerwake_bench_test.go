package server

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/entity"
)

// BenchmarkRegisterWakeEmptyHerd times the per-tick wake registration when
// nothing woke, beside a large herd with no vex.
func BenchmarkRegisterWakeEmptyHerd(b *testing.B) {
	s, _ := newTrackerBenchServer(b)
	dim := s.allDims()[0]
	for i := range 2000 {
		s.spawnMobInDim(dim, entity.TypeCow, float64(i%40)*2-40, 65, float64(i/40)*2-50, int64(i), nil)
	}
	b.ResetTimer()
	for range b.N {
		s.registerWake(wakeResult{})
	}
}
