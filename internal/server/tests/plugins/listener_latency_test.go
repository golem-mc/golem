package plugins_test

import (
	"sync/atomic"
	"testing"
	"time"

	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/plugin"
)

// A move a plugin listens to is handled between ticks: its listener runs
// and the packets behind it (here a ping) are answered without waiting for
// the next tick, which at one tick a second is a second away.
// vanilla: MinecraftServer.waitUntilNextTick
func TestPluginMoveListenerDoesNotWaitForTheNextTick(t *testing.T) {
	var host plugin.Server
	srv := servertest.NewTestServerWithPlugins(t, servertest.BlockTestConfig(),
		[]plugin.Registration{{Name: "afk", Plugin: &testPlugin{enable: func(s plugin.Server) error { host = s; return nil }}}})
	c := servertest.DialServer(t, srv)
	c.Join("Mover")
	var heard atomic.Bool
	ready := make(chan struct{})
	host.Scheduler().RunTask(func() {
		plugin.Listen(host, func(*plugin.PlayerMoveEvent) { heard.Store(true) })
		close(ready)
	})
	<-ready
	srv.RunLine("tick rate 1")
	// Start just after a tick, a second before the next: this test is about
	// wall time between ticks, so it polls the counter rather than counting
	// ticks. The first tick after the change may still be on the old pacing.
	for range 2 {
		for t0 := srv.TicksRun(); srv.TicksRun() == t0; {
			time.Sleep(time.Millisecond)
		}
	}
	from := c.Pos()
	sent := time.Now()
	c.MoveToGrounded(from.X+0.5, from.Y, from.Z, 0, 0, true)
	c.Settle()
	if took := time.Since(sent); took > 500*time.Millisecond {
		t.Fatalf("the ping behind the move was answered after %v, want well within the second to the next tick", took)
	}
	if !heard.Load() {
		t.Fatal("the move listener did not run")
	}
}
