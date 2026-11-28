package server

import (
	"fmt"
	"testing"

	"github.com/golem-mc/golem/internal/server/command"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/google/uuid"
)

// BenchmarkEntityQueryPlayers runs one entity query over 20 markers with 20
// players online; the queries rule players out by type or UUID.
func BenchmarkEntityQueryPlayers(b *testing.B) {
	s, _ := newTrackerBenchServer(b)
	dim := s.allDims()[0]
	s.mu.Lock()
	clear(s.clients)
	for i := range 20 {
		p := &player.Player{UUID: uuid.New(), Name: fmt.Sprintf("P%d", i), EntityID: int32(i + 1)}
		c := &client{srv: s, player: p}
		c.dim.Store(dim)
		c.worldScope = worldScope{srv: s, in: c.dimension, by: c.Profile}
		s.assignPlayerSlot(c)
		s.clients[p.UUID] = c
	}
	s.mu.Unlock()
	for range 20 {
		s.RunRconLine("summon marker 0 -60 0")
	}
	ws := worldScope{srv: s, in: func() *dimension { return dim }}
	missing := uuid.New()
	for _, q := range []struct {
		name string
		q    command.EntityQuery
	}{
		{"type", command.EntityQuery{Type: "minecraft:marker"}},
		{"uuid", command.EntityQuery{UUID: &missing}},
	} {
		b.Run(q.name, func(b *testing.B) {
			for b.Loop() {
				ws.Entities(q.q)
			}
		})
	}
}
