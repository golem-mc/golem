package server

import (
	"fmt"
	"github.com/golem-mc/golem/internal/config"
	"log/slog"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/world"
)

// BenchmarkChunkViewMoves shifts a ten-chunk view back and forth across a
// chunk border over cached terrain, including holds, visibility and send queues.
// It runs for a client asking for a distance of 1 and for one asking for the
// server's ten.
func BenchmarkChunkViewMoves(b *testing.B) {
	for _, asked := range []int32{1, 10} {
		b.Run(fmt.Sprintf("ask%d", asked), func(b *testing.B) { benchChunkViewMoves(b, asked) })
	}
}

func benchChunkViewMoves(b *testing.B, asked int32) {
	s, w := newBlockBenchServer(b, 0)
	s.viewDistance.Store(10)
	s.simulationDistance.Store(10)
	var holds []world.ChunkPos
	for x := int32(-10); x <= 11; x++ {
		for z := int32(-10); z <= 10; z++ {
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
	c.viewDistance.Store(max(asked, config.MinDistance))
	c.dim.Store(s.allDims()[0])
	b.Cleanup(func() { c.forgetChunks(nil) })
	if err := c.updateChunks(); err != nil {
		b.Fatal(err)
	}
	if len(c.generating) != 0 || len(c.pendingGeneration) != 0 {
		b.Fatal("benchmark terrain was not cached")
	}
	// The client knows every chunk in the initial view, so each move also
	// exercises unloading sent chunks and updating the concurrent sent mirror.
	for x := int32(-10); x <= 10; x++ {
		for z := int32(-10); z <= 10; z++ {
			c.markSent(world.ChunkPos{X: x, Z: z})
		}
	}
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
