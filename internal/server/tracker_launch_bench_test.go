package server

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/plugin"
	"github.com/golem-mc/golem/registry"
	"github.com/google/uuid"
)

type launchBenchPlugin struct{ calls int }

func (p *launchBenchPlugin) Enable(s plugin.Server) error {
	plugin.Listen(s, func(*plugin.ProjectileLaunchEvent) { p.calls++ })
	return nil
}

func (*launchBenchPlugin) Disable() {}

// BenchmarkLaunchFireworkWatched times a rocket's launch, through the spawn
// packets queued to the players in range of it, with no listener and with a
// launch listener that accepts it. The rockets are cleared and the watchers'
// queues emptied every 256 launches, outside the timed section.
func BenchmarkLaunchFireworkWatched(b *testing.B) {
	run := func(b *testing.B, listen bool) {
		s, w := newTrackerBenchServer(b)
		s.deferWakes.Store(true)
		for _, c := range s.clients {
			c.out = &mailbox{c: c, wake: make(chan struct{}, 1), done: make(chan struct{})}
			c.worldScope = worldScope{srv: s, in: c.dimension, by: c.Profile}
		}
		fixture := &launchBenchPlugin{}
		if listen {
			s.loadPlugins([]plugin.Registration{{Name: "bench", Plugin: fixture}})
			s.enablePlugins()
			s.DriveTicksHere()
		}
		stack := protocol.ItemStack{ItemID: int32(registry.ItemFireworkRocket), Count: 1}
		x, z := trackerBenchSpot(0)
		reset := func() {
			for _, it := range append([]*fireworkEntity(nil), s.fireworks...) {
				s.killFirework(it)
			}
			for _, c := range s.clients {
				c.out.queue, c.out.queued = c.out.queue[:0], 0
			}
		}
		b.ReportAllocs()
		b.ResetTimer()
		for i := range b.N {
			if i%256 == 255 {
				b.StopTimer()
				reset()
				b.StartTimer()
			}
			s.spawnFirework(w, stack, x, float64(world.GroundLevel)+5, z, 0, uuid.Nil, 0)
		}
		b.StopTimer()
		if listen && fixture.calls != b.N {
			b.Fatalf("launch listener calls = %d, want %d", fixture.calls, b.N)
		}
		queued := 0
		for _, c := range s.clients {
			queued += len(c.out.queue)
		}
		if queued == 0 {
			b.Fatal("no spawn packet was queued")
		}
	}
	b.Run("plain", func(b *testing.B) { run(b, false) })
	b.Run("listener", func(b *testing.B) { run(b, true) })
}
