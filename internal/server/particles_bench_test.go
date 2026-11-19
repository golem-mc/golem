package server

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/google/uuid"
)

// BenchmarkSendParticlePayload covers recipient collection, filtering and mailbox enqueue.
func BenchmarkSendParticlePayload(b *testing.B) {
	for _, n := range []int{8, 64} {
		b.Run(fmt.Sprintf("players=%d", n), func(b *testing.B) {
			s := &Server{clients: make(map[uuid.UUID]*client, n)}
			dim := &dimension{w: &world.World{}}
			other := &dimension{w: &world.World{}}
			clients := make([]*client, n)
			s.deferWakes.Store(true)
			for i := range n {
				p := &player.Player{UUID: uuid.New()}
				p.SetPosition(player.Position{X: float64(i%16) - 8, Y: 64, Z: float64(i % 8)})
				c := &client{srv: s, player: p, out: &mailbox{queue: make([]outbound, 0, 1)}}
				c.dim.Store(dim)
				// Half receive the burst, a quarter are exactly at the strict
				// 32-block boundary, and a quarter are in another dimension.
				if i >= n/2 && i < 3*n/4 {
					p.SetPosition(player.Position{X: 32.9, Y: 64, Z: 0})
				} else if i >= 3*n/4 {
					c.dim.Store(other)
				}
				clients[i] = c
				s.clients[p.UUID] = c
			}
			data := particlesPayload(protocol.ParticleFlame, false, .5, 64.5, .5, .1, .2, .3, .5, 7)
			s.sendParticlePayload(dim.w, data, .5, 64.5, .5)
			for i, c := range clients {
				want := 0
				if i < n/2 {
					want = 1
				}
				if len(c.out.queue) != want {
					b.Fatalf("client %d: got %d packets, want %d", i, len(c.out.queue), want)
				}
				if want == 1 && (c.out.queue[0].id != protocol.IDPlayLevelParticles || !bytes.Equal(c.out.queue[0].data, data)) {
					b.Fatalf("client %d: incorrect particle packet", i)
				}
				clear(c.out.queue)
				c.out.queue = c.out.queue[:0]
				c.out.queued = 0
			}
			b.ReportAllocs()
			for b.Loop() {
				s.sendParticlePayload(dim.w, data, .5, 64.5, .5)
				// Reuse mailbox storage without writer goroutines or socket I/O.
				for _, c := range clients[:n/2] {
					clear(c.out.queue)
					c.out.queue = c.out.queue[:0]
					c.out.queued = 0
				}
			}
		})
	}
}
