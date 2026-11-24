package server

import (
	"log/slog"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/world"
)

// BenchmarkChunkViewMoves shifts a ten-chunk view back and forth across a
// chunk border over cached terrain, including holds, visibility and send queues.
func BenchmarkChunkViewMoves(b *testing.B) {
	s, w := newBlockBenchServer(b, 0)
	s.viewDistance.Store(10)
	s.simulationDistance.Store(10)
	var holds []world.ChunkPos
	// Both centers' send views, which reach one chunk past the radius.
	for x := int32(-11); x <= 12; x++ {
		for z := int32(-11); z <= 11; z++ {
			p := world.ChunkPos{X: x, Z: z}
			w.Acquire(p)
			holds = append(holds, p)
		}
	}
	b.Cleanup(func() {
		for _, p := range holds {
			w.Release(p)
		}
	})
	c := &client{srv: s, player: &player.Player{}, conn: protocol.NewConn(new(playerTrackingBenchConn)), log: slog.New(slog.DiscardHandler)}
	c.dim.Store(s.allDims()[0])
	b.Cleanup(func() { c.forgetChunks(nil) })
	if err := c.updateChunks(); err != nil {
		b.Fatal(err)
	}
	if len(c.generating) != 0 || len(c.pendingGeneration) != 0 {
		b.Fatal("benchmark terrain was not cached")
	}
	// Load-only holds are not what a move costs, so the queue starts empty.
	c.pendingHolds = nil
	// The client knows every chunk in the initial send view, so each move also
	// exercises unloading sent chunks and updating the concurrent sent mirror.
	for _, p := range c.pendingSend {
		c.markSent(p)
	}
	c.pendingSend = nil
	b.ReportAllocs()
	b.ResetTimer()
	for i := range b.N {
		c.player.SetPosition(player.Position{X: float64((i+1)%2) * 16, Y: 64})
		if err := c.updateChunks(); err != nil {
			b.Fatal(err)
		}
		for _, p := range c.pendingSend {
			c.markSent(p)
		}
	}
}
