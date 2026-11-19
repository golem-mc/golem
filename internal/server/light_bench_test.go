package server

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/google/uuid"
)

// BenchmarkBroadcastLight measures one changed sky section reaching twenty
// players, half on the view border and half tracking all eight neighbours.
func BenchmarkBroadcastLight(b *testing.B) {
	s, w := newBlockBenchServer(b, 0)
	dim := s.dimensionOfWorld(w)
	clients := make([]*client, 20)
	for i := range clients {
		p := &player.Player{UUID: uuid.New()}
		c := &client{srv: s, player: p, sentViewWorld: w, sentView: newChunkSet(world.ChunkPos{}, 8)}
		c.dim.Store(dim)
		for x := int32(-1); x <= 1; x++ {
			for z := int32(-1); z <= 1; z++ {
				if i%2 == 0 || x >= 0 {
					c.sentView.add(world.ChunkPos{X: x, Z: z})
				}
			}
		}
		// Drain the queues directly to measure selection, encoding and enqueue
		// without a network writer's scheduling or socket cost.
		c.out = &mailbox{c: c, wake: make(chan struct{}, 1)}
		s.assignPlayerSlot(c)
		s.clients[p.UUID] = c
		clients[i] = c
	}
	s.deferWakes.Store(true)
	changed := []world.LightChange{{Pos: world.ChunkPos{}, Sky: 1 << 17}}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		s.broadcastLight(w, changed)
		for _, c := range clients {
			clear(c.out.queue)
			c.out.queue = c.out.queue[:0]
			c.out.queued = 0
		}
	}
}
