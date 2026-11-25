package server

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
	"github.com/google/uuid"
)

func TestRegisterPairsOnlyForEligibleWatchers(t *testing.T) {
	for _, tt := range []struct {
		name      string
		watcherX  float64
		wantCalls int
	}{
		{name: "out of range", watcherX: 1000, wantCalls: 0},
		{name: "two in range", watcherX: 0.5, wantCalls: 1},
	} {
		t.Run(tt.name, func(t *testing.T) {
			dim := &dimension{w: &world.World{}}
			s := &Server{clients: make(map[uuid.UUID]*client)}
			s.viewDistance.Store(4)
			s.entityBroadcastRange.Store(100)
			for range 2 {
				p := &player.Player{UUID: uuid.New()}
				p.SetPosition(player.Position{X: tt.watcherX, Y: 70, Z: 0.5})
				c := &client{srv: s, player: p}
				c.dim.Store(dim)
				s.assignPlayerSlot(c)
				s.clients[p.UUID] = c
			}

			stack := protocol.ItemStack{ItemID: int32(registry.ItemFireworkRocket), Count: 1}
			rocket := s.newFirework(dim.w, stack, 0.5, 70, 0.5, 0, uuid.Nil, 0)
			tracker := fireworkTracker
			calls := 0
			tracker.pairing = func(it *fireworkEntity) []outbound {
				calls++
				return fireworkPairing(it)
			}
			tracker.register(s, rocket)

			if calls != tt.wantCalls {
				t.Errorf("pairing called %d times, want %d", calls, tt.wantCalls)
			}
			if err := tracker.chunkDrift(s); err != nil {
				t.Fatal(err)
			}
			if len(s.fireworks) != 1 || s.fireworks[0] != rocket {
				t.Fatal("rocket missing from the registered list")
			}
			for _, c := range s.clients {
				if _, marked := rocket.tracked.get(c); marked != (tt.wantCalls > 0) {
					t.Errorf("watcher at x=%v: marked=%v", tt.watcherX, marked)
				}
			}
		})
	}
}
