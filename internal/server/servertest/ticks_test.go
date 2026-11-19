package servertest_test

import (
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
)

func offline() *config.Config {
	cfg := config.Default()
	cfg.OnlineMode = false
	return cfg
}

// TestHeldLoopRunsTheTicksAsked holds a server's loop and runs it by hand:
// each RunTicks runs exactly the ticks it names, and a barrier read runs
// none.
func TestHeldLoopRunsTheTicksAsked(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, offline(), "Testificate")

	servertest.HoldTicks(t, srv)
	before := srv.TicksRun()
	servertest.RunTicks(t, srv, 5)
	if got := srv.TicksRun() - before; got != 5 {
		t.Fatalf("RunTicks(5) ran %d ticks, want 5", got)
	}
	c.ExpectNoneWithinTicks(srv, 0, servertest.PacketIs(protocol.IDPlayPlayerChat))
	if got := srv.TicksRun() - before; got != 5 {
		t.Fatalf("a barrier alone ran %d ticks, want none", got-5)
	}
	servertest.RunTicks(t, srv, 100)
	if got := srv.TicksRun() - before; got != 105 {
		t.Fatalf("RunTicks(100) ran %d ticks, want 100", got-5)
	}
}

// TestHeldReadStopsAtTheTick reads for a time sync on a held loop: it comes
// on the twentieth tick, and the read runs no tick past the one that sent
// it.
func TestHeldReadStopsAtTheTick(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, offline(), "Testificate")

	servertest.HoldTicks(t, srv)
	c.Settle()
	// Line the read up on a sync, so the next comes a whole interval on.
	// The first is due once twenty ticks have finished, so on the 21st.
	c.ExpectWithinTicks(srv, 21, protocol.IDPlayUpdateTime)
	before := srv.TicksRun()
	c.ExpectWithinTicks(srv, 40, protocol.IDPlayUpdateTime)
	if got := srv.TicksRun() - before; got != 20 {
		t.Fatalf("the next time sync came %d ticks on, want 20", got)
	}
}
