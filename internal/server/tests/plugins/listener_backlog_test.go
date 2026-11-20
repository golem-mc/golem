package plugins_test

import (
	"testing"
	"time"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/plugin"
)

// A connection whose moves a plugin listens to keeps up with the client
// while ticks overrun: a command sent behind 5 s of moves is answered within
// about a tick, not behind a backlog of one move per tick.
// vanilla: MinecraftServer.tickServer runs every packet received since the last tick
// Slow by design: 5 s of moves at the client's rate is the scenario.
func TestPluginMoveListenerKeepsUpWithSlowTicks(t *testing.T) {
	var host plugin.Server
	srv := servertest.NewTestServerWithPlugins(t, servertest.BlockTestConfig(),
		[]plugin.Registration{{Name: "afk", Plugin: &testPlugin{enable: func(s plugin.Server) error { host = s; return nil }}}})
	c := servertest.DialServer(t, srv)
	c.Join("Mover")
	ready := make(chan struct{})
	host.Scheduler().RunTask(func() {
		plugin.Listen(host, func(*plugin.PlayerMoveEvent) {})
		// Every tick takes about 80 ms, past its 50 ms budget.
		host.Scheduler().RunTimer(0, 1, func() { time.Sleep(80 * time.Millisecond) })
		close(ready)
	})
	<-ready
	from := c.Pos()
	for i := range 100 {
		c.MoveToGrounded(from.X+0.1*float64(i%2), from.Y, from.Z, 0, 0, true)
		time.Sleep(50 * time.Millisecond)
	}
	sent := srv.TicksRun()
	c.ChatCommand("list")
	for id := range c.Packets(10 * time.Second) {
		if id != protocol.IDPlaySystemChat {
			continue
		}
		if took := srv.TicksRun() - sent; took > 3 {
			t.Fatalf("the command behind the moves was answered %d ticks after it was sent, want at most 3", took)
		}
		return
	}
	t.Fatal("the command behind the moves was not answered")
}
