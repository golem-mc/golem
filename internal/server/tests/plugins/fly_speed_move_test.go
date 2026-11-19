package plugins_test

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/plugin"
)

// A player a plugin made fly ten times faster covers ten times vanilla's
// ground per move without the moved-too-quickly check sending them back,
// and is still sent back past that. vanilla-bukkit: Spigot's
// movedTooQuicklyMultiplier scaling
func TestPluginFlySpeedWidensTheMoveCheck(t *testing.T) {
	var host plugin.Server
	srv := servertest.NewTestServerWithPlugins(t, servertest.SurvivalTestConfig(),
		[]plugin.Registration{{Name: "speed", Plugin: &testPlugin{enable: func(s plugin.Server) error { host = s; return nil }}}},
		func(s *server.Server) { s.SetMoveChecks(true) })
	c := servertest.DialServer(t, srv)
	c.Join("Swift")
	p := handle(t, host, "Swift")
	done := make(chan struct{})
	host.Scheduler().RunTask(func() {
		p.SetAllowFlight(true)
		p.SetFlying(true)
		p.SetFlySpeed(1) // Bukkit's top speed: ten times the default 0.1
		close(done)
	})
	<-done

	from := c.Pos()
	// 30 blocks is past vanilla's 10 a packet and within ten times that.
	c.MoveToGrounded(from.X+30, from.Y+2, from.Z, 0, 0, false)
	c.ReadTicks(srv, servertest.QuietTicks, func(id int32, _ []byte) bool {
		if id == protocol.IDPlaySyncPosition {
			t.Fatalf("a 30-block step at fly speed 1 was sent back to %v", c.LastTeleport)
		}
		return false
	})
	c.MoveToGrounded(from.X+30+150, from.Y+2, from.Z, 0, 0, false)
	if got := servertest.ExpectTeleport(t, c); got.X != from.X+30 {
		t.Fatalf("a 150-block step sent back to x=%v, want %v", got.X, from.X+30)
	}
}
