package server

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/world"
)

// BenchmarkReconnectShapesPlain times the reconnect pass for a changed cell
// with no shape-reactive block nearby, the case of nearly every redstone,
// piston and fluid change.
func BenchmarkReconnectShapesPlain(b *testing.B) {
	s, w := newTrackerBenchServer(b)
	for b.Loop() {
		s.reconnectShapes(w, 8, world.GroundLevel, 8)
	}
}
