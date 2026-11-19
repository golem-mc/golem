package server

import (
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/world"
	"testing"
)

// BenchmarkNaturalSpawning runs every natural spawn category over 36 lit,
// flat stone chunks with eight players; the ground cannot support land spawns.
func BenchmarkNaturalSpawning(b *testing.B) {
	s, _ := newTrackerBenchServer(b)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		s.tickNaturalSpawning()
	}
}

// BenchmarkNaturalSpawningHerd runs natural spawning beside two thousand
// zombies that count toward the monster cap.
func BenchmarkNaturalSpawningHerd(b *testing.B) {
	s, _ := newTrackerBenchServer(b)
	dim := s.allDims()[0]
	for i := range 2000 {
		s.spawnMobInDim(dim, entity.TypeZombie, float64(i%45)*2-45, float64(world.GroundLevel), float64(i/45)*2-45, int64(i), nil)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		s.tickNaturalSpawning()
	}
}
