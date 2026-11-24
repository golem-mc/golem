package server

import (
	"fmt"
	"log/slog"
	"testing"
	"time"

	"github.com/golem-mc/golem/internal/config"
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
		b.Run(fmt.Sprintf("ask%d", asked), func(b *testing.B) { benchChunkViewMoves(b, asked, true) })
	}
}

// BenchmarkChunkViewMovesWithPendingHolds moves a player who asks for render
// distance one while the simulation-distance holds are still queued.
func BenchmarkChunkViewMovesWithPendingHolds(b *testing.B) {
	benchChunkViewMoves(b, 1, false)
}

func benchChunkViewMoves(b *testing.B, asked int32, settle bool) {
	const load = 10
	s, w := newBlockBenchServer(b, 0)
	s.viewDistance.Store(load)
	s.simulationDistance.Store(load)
	radius := max(asked, config.MinDistance)
	cacheRadius := int32(load)
	if !settle {
		cacheRadius = radius
	}
	var holds []world.ChunkPos
	// Both centers' send views, which reach one chunk past the radius.
	for x := -cacheRadius - 1; x <= cacheRadius+2; x++ {
		for z := -cacheRadius - 1; z <= cacheRadius+1; z++ {
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
	c.viewDistance.Store(radius)
	c.dim.Store(s.allDims()[0])
	b.Cleanup(func() { c.forgetChunks(nil) })
	if err := c.updateChunks(); err != nil {
		b.Fatal(err)
	}
	if len(c.generating) != 0 || len(c.pendingGeneration) != 0 {
		b.Fatal("benchmark terrain was not cached")
	}
	initialSend := len(c.pendingSend)
	for _, p := range c.pendingSend {
		c.markSent(p)
	}
	c.pendingSend = nil
	if settle {
		// Take the load-radius holds before measuring movement from a settled view.
		for range 10000 {
			if len(c.pendingHolds) == 0 && len(c.generating) == 0 {
				break
			}
			c.takeHolds()
			c.collectGenerated()
			time.Sleep(time.Millisecond)
			for _, p := range c.pendingSend {
				c.markSent(p)
			}
			c.pendingSend = nil
		}
		if len(c.pendingHolds) != 0 || len(c.generating) != 0 {
			b.Fatal("load holds did not settle")
		}
	} else if want := (2*load+1)*(2*load+1) - initialSend; len(c.pendingHolds) != want {
		b.Fatalf("queued holds = %d, want %d", len(c.pendingHolds), want)
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
