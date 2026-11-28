package server

import (
	"log/slog"
	"testing"
	"time"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
	"github.com/google/uuid"
)

func addBlockBenchPlayers(b testing.TB, s *Server, w *world.World, n int, lo, hi int32) {
	dim := s.dimensionOfWorld(w)
	for i := range n {
		p := &player.Player{UUID: uuid.New(), EntityID: s.nextEntity()}
		p.SetPosition(player.Position{X: float64(i), Y: float64(world.GroundLevel) + 30, Z: 0})
		c := &client{
			srv:    s,
			conn:   protocol.NewConn(&playerTrackingBenchConn{sentinel: make(chan struct{}, 1), done: make(chan struct{})}),
			log:    slog.New(slog.DiscardHandler),
			player: p,
		}
		c.dim.Store(dim)
		c.out = c.startMailbox()
		c.out.release()
		b.Cleanup(func() { c.out.stop() })
		c.sentView, c.sentViewWorld = newChunkSet(world.ChunkPos{}, max(world.Abs32(lo), world.Abs32(hi))), w
		for x := lo; x <= hi; x++ {
			for z := lo; z <= hi; z++ {
				c.sentView.add(world.ChunkPos{X: x, Z: z})
			}
		}
		s.mu.Lock()
		s.assignPlayerSlot(c)
		s.clients[p.UUID] = c
		s.mu.Unlock()
	}
}

// BenchmarkWaterFloodServer is 200 server ticks of 36 water sources flooding
// a flat world, with the block changes relit and sent to the joined players.
// It reports how a tick divides between the engine, what follows a change
// (relight booking, shape reconnects, portal and frame rechecks), the wait
// for the light pool, and the flush that builds and queues the packets.
func BenchmarkWaterFloodServer(b *testing.B) {
	for _, players := range []int{0, 1, 20} {
		b.Run(map[int]string{0: "players0", 1: "players1", 20: "players20"}[players], func(b *testing.B) {
			s, w := newBlockBenchServer(b, 0)
			for cx := int32(-5); cx <= 5; cx++ {
				for cz := int32(-5); cz <= 5; cz++ {
					w.Acquire(world.ChunkPos{X: cx, Z: cz})
				}
			}
			w.FlushLighting()
			addBlockBenchPlayers(b, s, w, players, -5, 5)
			s.backstopMu.Lock()
			s.tickRunning = true
			s.backstopMu.Unlock()
			b.Cleanup(func() {
				s.backstopMu.Lock()
				s.tickRunning = false
				s.backstopMu.Unlock()
			})
			dim := s.dimensionOfWorld(w)
			source := registry.MustState("minecraft:water", nil)
			var engine, after, light, flush time.Duration
			var changed int
			var tick int64
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				b.StopTimer()
				for x := int32(-40); x <= 40; x += 16 {
					for z := int32(-40); z <= 40; z += 16 {
						w.SetBlock(x, world.GroundLevel+16, z, source)
						dim.physics.NotifyPlaced(x, world.GroundLevel+16, z, 0)
					}
				}
				b.StartTimer()
				for range 200 {
					tick++
					t0 := time.Now()
					changes, falls, primes, effects := dim.physics.Tick(tick)
					t1 := time.Now()
					s.physicsResults(dim, changes, falls, primes, effects)
					t2 := time.Now()
					w.FlushLighting()
					s.relightWG.Wait()
					t3 := time.Now()
					s.flushAllBlockChanges()
					t4 := time.Now()
					engine += t1.Sub(t0)
					after += t2.Sub(t1)
					light += t3.Sub(t2)
					flush += t4.Sub(t3)
					changed += len(changes)
				}
				b.StopTimer()
				for x := int32(-72); x <= 72; x++ {
					for z := int32(-72); z <= 72; z++ {
						for y := int32(world.GroundLevel); y <= world.GroundLevel+17; y++ {
							if registry.BlockOf(w.BlockAt(x, y, z)) == registry.BlockWater {
								w.SetBlock(x, y, z, 0)
							}
						}
					}
				}
				for range 40 {
					tick++
					dim.physics.Tick(tick)
				}
				w.TakeChanged()
				w.FlushLighting()
				s.relightWG.Wait()
				w.TakeLightChanged()
				b.StartTimer()
			}
			b.StopTimer()
			n := float64(b.N)
			b.ReportMetric(float64(engine.Microseconds())/n, "engine-us")
			b.ReportMetric(float64(after.Microseconds())/n, "after-us")
			b.ReportMetric(float64(light.Microseconds())/n, "lightwait-us")
			b.ReportMetric(float64(flush.Microseconds())/n, "flush-us")
			b.ReportMetric(float64(changed)/n, "changes")
		})
	}
}
