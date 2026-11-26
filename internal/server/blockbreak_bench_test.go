package server

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
	"github.com/google/uuid"
)

// BenchmarkSurvivalBlockBreak breaks dirt by hand in survival with seven
// observers, so the block's loot roll builds its context each iteration.
func BenchmarkSurvivalBlockBreak(b *testing.B) {
	s, w := newCommandBenchServer(b, 0)
	var clients []*client
	for range 8 {
		p := &player.Player{UUID: uuid.New(), EntityID: s.nextEntity()}
		p.SetPosition(player.Position{X: 2.5, Y: float64(world.GroundLevel), Z: 4.5})
		c := &client{srv: s, player: p, sentViewWorld: w, sentView: newChunkSet(world.ChunkPos{}, 2)}
		c.sentView.add(world.ChunkPos{})
		c.dim.Store(s.allDims()[0])
		c.out = &mailbox{c: c, wake: make(chan struct{}, 1)}
		s.assignPlayerSlot(c)
		s.clients[p.UUID] = c
		clients = append(clients, c)
	}
	// Synthetic clients have no sockets to close during server shutdown.
	b.Cleanup(func() {
		s.relightWG.Wait()
		s.mu.Lock()
		clear(s.clients)
		s.mu.Unlock()
	})
	clients[0].player.UpdateVitals(func(v *player.Vitals) { v.GameMode = protocol.GameModeSurvival })
	s.deferWakes.Store(true)
	y := int32(world.GroundLevel)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		w.SetBlock(4, y, 4, registry.BlockDirt.DefaultState())
		clients[0].destroyBlock(4, y, 4, false)
		s.flushBlockChanges(w)
		for _, c := range clients {
			c.out.mu.Lock()
			clear(c.out.queue)
			c.out.queue = c.out.queue[:0]
			c.out.queued = 0
			c.out.mu.Unlock()
		}
	}
}
