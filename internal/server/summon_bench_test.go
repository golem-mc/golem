package server

import (
	"fmt"
	"log/slog"
	"testing"
)

// BenchmarkSummonBesideMarkers summons and kills a tagged-UUID marker beside
// 2000 others, so the duplicate-UUID check walks a full world.
func BenchmarkSummonBesideMarkers(b *testing.B) {
	s := newMarkersBenchServer(b)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		s.RunRconLine("summon marker 0 -50 0 {UUID:[I;1,2,3,4]}")
		s.RunRconLine("kill 00000001-0000-0002-0000-000300000004")
	}
	b.StopTimer()
	if n := s.liveFixtureCount(); n != 2000 {
		b.Fatalf("%d markers after the loop, want 2000", n)
	}
}

// BenchmarkReadMobTickInputsBesideMarkers reads the mob tick's inputs, which
// gather every live non-mob UUID, beside 2000 markers and no mobs.
func BenchmarkReadMobTickInputsBesideMarkers(b *testing.B) {
	s := newMarkersBenchServer(b)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		in := s.resetMobTickInputs()
		s.readMobTickInputs(in)
	}
}

// newMarkersBenchServer is a tracker bench server with no players and 2000
// live markers.
func newMarkersBenchServer(b *testing.B) *Server {
	s, _ := newTrackerBenchServer(b)
	s.mu.Lock()
	clear(s.clients)
	s.mu.Unlock()
	logger := slog.Default()
	slog.SetDefault(slog.New(slog.DiscardHandler))
	b.Cleanup(func() { slog.SetDefault(logger) })
	// 2000 markers over the held chunks -48..47 on each axis.
	for i := range 2000 {
		s.RunRconLine(fmt.Sprintf("summon marker %d -60 %d", i%80-40, i/80*3-40))
	}
	if n := s.liveFixtureCount(); n != 2000 {
		b.Fatalf("%d markers summoned, want 2000", n)
	}
	return s
}

func (s *Server) liveFixtureCount() int {
	s.fixturesMu.Lock()
	defer s.fixturesMu.Unlock()
	n := 0
	for _, ent := range s.fixtures {
		if !ent.dead {
			n++
		}
	}
	return n
}
