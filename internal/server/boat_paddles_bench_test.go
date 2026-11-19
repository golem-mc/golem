package server

import (
	"log/slog"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// BenchmarkTickBoatsPaddling includes eight occupied hulls with both paddles
// active, sound collection and delivery through all eight riders' mailboxes.
func BenchmarkTickBoatsPaddling(b *testing.B) {
	for _, surface := range []string{"water", "land"} {
		b.Run(surface, func(b *testing.B) {
			s, w := newTrackerBenchServer(b)
			y := float64(world.GroundLevel)
			if surface == "water" {
				water := registry.MustState("minecraft:water", nil)
				for x := int32(-10); x <= 10; x++ {
					for z := int32(-10); z <= 10; z++ {
						w.SetBlock(x, world.GroundLevel, z, water)
					}
				}
				y += 0.5
			}
			var riders []*client
			var sentinels []chan struct{}
			for _, c := range s.clients {
				i := len(riders)
				x, z := float64(i%4)*3-4.5, float64(i/4)*3-1.5
				c.player.SetPosition(player.Position{X: x, Y: y, Z: z})
				conn := &playerTrackingBenchConn{sentinel: make(chan struct{}, 1), done: make(chan struct{})}
				c.conn = protocol.NewConn(conn)
				c.log = slog.New(slog.DiscardHandler)
				c.out = c.startMailbox()
				c.out.release()
				b.Cleanup(func() {
					conn.Close()
					c.out.stop()
				})
				it := s.spawnBoat(w, "minecraft:oak_boat", x, y, z, 0, customName{})
				it.passengers = []*boatPassenger{{c: c}}
				c.vehicle.Store(it)
				it.boat.OnGround = surface == "land"
				it.boat.LeftPaddle, it.boat.RightPaddle = true, true
				riders = append(riders, c)
				sentinels = append(sentinels, conn.sentinel)
			}
			// Wait for every mailbox to write the tick's effects before the
			// next iteration, so queue growth cannot hide delivery costs.
			tick := func() {
				s.tickBoats()
				for _, c := range riders {
					var marker protocol.Buffer
					marker.PutI64(0x3321332133213321)
					c.send(protocol.IDPlayPong, marker.Bytes())
				}
				for _, sentinel := range sentinels {
					<-sentinel
				}
			}
			for range 40 {
				tick()
			}
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				tick()
			}
			b.StopTimer()
			for _, it := range s.boats {
				if it.driver() == nil || !it.boat.LeftPaddle || !it.boat.RightPaddle {
					b.Fatal("boat lost its rower or active paddles")
				}
			}
		})
	}
}
