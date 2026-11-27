package server

import (
	"fmt"
	"testing"
)

// BenchmarkTickThrowablesIdleCarts runs the thrown tick over idle minecarts
// with no thrown body anywhere.
func BenchmarkTickThrowablesIdleCarts(b *testing.B) {
	s, _ := newTrackerBenchServer(b)
	for i := range 2000 {
		x, z := float64(i%45)*2-45, float64(i/45)*2-45
		s.RunRconLine(fmt.Sprintf("summon minecart %v 64 %v", x, z))
	}
	for range 40 {
		s.tickThrowables()
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		s.tickThrowables()
	}
}
