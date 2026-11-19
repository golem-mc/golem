package server

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/google/uuid"
)

func trackingAreaFixture(players, bodies int, spread bool) (*Server, []*client) {
	s := &Server{clients: make(map[uuid.UUID]*client)}
	s.viewDistance.Store(10)
	s.entityBroadcastRange.Store(100)
	dim := &dimension{w: new(world.World)}
	clients := make([]*client, players)
	for i := range clients {
		id := uuid.UUID{15: byte(i + 1)}
		c := &client{srv: s, player: &player.Player{UUID: id, EntityID: int32(i + 1)}}
		x, z := float64(i%5)*2, float64(i/5)*2
		if spread {
			x, z = float64(i%10)*1024, float64(i/10)*1024
		}
		c.player.SetPosition(player.Position{X: x, Y: 64, Z: z})
		c.dim.Store(dim)
		s.assignPlayerSlot(c)
		s.clients[id] = c
		clients[i] = c
	}
	for i := range bodies {
		p := clients[i%players].player.Position()
		s.paintings = append(s.paintings, newPainting(int32(100+i), uuid.UUID{14: byte(i >> 8), 15: byte(i)}, dim.w,
			int32(p.X)+int32(i%4), 65, int32(p.Z)+int32(i%3), 3, 0, 1, 1))
	}
	return s, clients
}

// BenchmarkTrackingAreaSpread measures the tracker tick with 2,000 bodies and
// 50 players a thousand blocks apart, including the watcher snapshot read.
func BenchmarkTrackingAreaSpread(b *testing.B) {
	benchmarkTrackingArea(b, true)
}

// BenchmarkTrackingAreaDense keeps the same population together to measure
// the index's overhead when every player must still be tested.
func BenchmarkTrackingAreaDense(b *testing.B) {
	benchmarkTrackingArea(b, false)
}

func benchmarkTrackingArea(b *testing.B, spread bool) {
	s, _ := trackingAreaFixture(50, 2000, spread)
	paintingTracker.frozenPass(s, s.trackingWatchers())
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		paintingTracker.frozenPass(s, s.trackingWatchers())
	}
}
