package server

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/player"
	"github.com/google/uuid"
)

// BenchmarkTickDeadBodies measures the scan over a hundred players, with and
// without a corpse expiring.
func BenchmarkTickDeadBodies(b *testing.B) {
	for _, name := range []string{"idle", "expiry"} {
		b.Run(name, func(b *testing.B) {
			s := &Server{clients: make(map[uuid.UUID]*client, 100)}
			dim := &dimension{}
			for i := range 100 {
				id := uuid.UUID{byte(i)}
				c := &client{srv: s, player: &player.Player{UUID: id}, out: &mailbox{}}
				c.out.released.Store(true)
				c.dim.Store(dim)
				s.clients[id] = c
			}
			c := s.clients[uuid.UUID{}]
			c.player.SetVitals(player.Vitals{Dead: name == "expiry"})
			b.ReportAllocs()
			for b.Loop() {
				c.deathTicks.Store(corpseTicks - 1)
				c.bodyGone.Store(false)
				c.holdsDropped.Store(false)
				s.tickDeadBodies()
			}
		})
	}
}
