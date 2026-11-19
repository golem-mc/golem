package server

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// BenchmarkCommandObserverPlacement places an observer and runs its pulse out.
func BenchmarkCommandObserverPlacement(b *testing.B) {
	s, w := newCommandBenchServer(b, 0)
	ws := worldScope{srv: s, in: func() *dimension { return s.allDims()[0] }}
	observer := registry.BlockObserver.DefaultState()
	p := s.physicsFor(w)
	b.ReportAllocs()
	for i := 0; b.Loop(); i++ {
		w.SetBlock(5, world.GroundLevel, 5, registry.BlockAir.DefaultState())
		if !ws.SetBlockTagged(5, world.GroundLevel, 5, observer, nil, nil, false, false) {
			b.Fatal("observer placement did not change the cell")
		}
		for tick := range int64(4) {
			p.Tick(int64(i)*4 + tick + 1)
		}
	}
}
