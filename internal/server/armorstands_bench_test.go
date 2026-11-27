package server

import (
	"fmt"
	"testing"
)

// BenchmarkTickMobsIdleStands runs the mob tick over armor stands with no mob
// anywhere.
func BenchmarkTickMobsIdleStands(b *testing.B) {
	s, _ := newTrackerBenchServer(b)
	for i := range 2000 {
		x, z := float64(i%45)*2-45, float64(i/45)*2-45
		s.RunRconLine(fmt.Sprintf("summon armor_stand %v 64 %v", x, z))
	}
	for range 40 {
		s.tickMobs()
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		s.tickMobs()
	}
}
