package server

import (
	"testing"
	"time"

	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/world"
)

func TestEndGatewayTicksInWorldWithForcedChunk(t *testing.T) {
	s, w := newTrackerBenchServer(t)
	y := int32(world.GroundLevel) + 5
	w.SetBlock(3, y, 3, rules.EndGatewayState)
	s.SetEndGatewayExit(3, y, 3, 0, 80, 0, false)
	if len(w.TickingCarriedOfKind("minecraft:end_gateway")) != 1 {
		t.Fatal("gateway not ticking")
	}
	w.SetChunkForced(world.ChunkPos{X: 0, Z: 0}, true)
	// No player stands near, so the filter reaches the forced and ticketed tests.
	s.mu.Lock()
	clear(s.clients)
	s.mu.Unlock()
	gate := s.newEntityGate()
	done := make(chan struct{})
	go func() {
		defer close(done)
		s.tickEndGateways(w, gate.blockFilter(w))
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("tickEndGateways deadlocked with a forced chunk")
	}
}
