package server

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/world"
)

// BenchmarkTickChickenLaying includes the gift roll, spawn and tracking of
// one hen laying each tick; discard its items so the workload stays fixed.
func BenchmarkTickChickenLaying(b *testing.B) {
	s, w := newTrackerBenchServer(b)
	id := s.spawnMobInDim(s.allDims()[0], entity.TypeChicken, 0.5, float64(world.GroundLevel), 0.5, 1, nil)
	m := s.mobByEntityIDLocked(w, id).mob
	m.NoAI = true
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		m.EggTimer = 1
		s.tickMobs()
		clear(s.items)
		s.items = s.items[:0]
	}
}
