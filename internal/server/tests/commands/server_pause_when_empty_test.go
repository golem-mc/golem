package commands

import (
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// emptyTicksAfter runs n held ticks on an empty server and returns how many
// of them advanced the world's age.
func emptyTicksAfter(t *testing.T, seconds, n int) int64 {
	t.Helper()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.PauseWhenEmptySeconds = seconds
	s := servertest.NewTestServer(t, cfg)
	servertest.HoldTicks(t, s)
	before, _ := s.Time()
	servertest.RunTicks(t, s, n)
	after, _ := s.Time()
	return after - before
}

// TestEmptyServerStopsTickingAfterPauseWhenEmpty checks the world's age stops
// once the server has stood empty for pause-when-empty-seconds: with 1 second
// the 20th empty tick is the first one skipped.
// vanilla: MinecraftServer.tickServer
func TestEmptyServerStopsTickingAfterPauseWhenEmpty(t *testing.T) {
	t.Parallel()
	advanced := emptyTicksAfter(t, 1, 60)
	if advanced != 19 {
		t.Errorf("world age advanced %d ticks in 60 empty ticks, want 19", advanced)
	}
}

// TestPauseWhenEmptyZeroNeverPauses checks 0 leaves the world ticking.
func TestPauseWhenEmptyZeroNeverPauses(t *testing.T) {
	t.Parallel()
	advanced := emptyTicksAfter(t, 0, 60)
	if advanced != 60 {
		t.Errorf("world age advanced %d ticks with pausing off, want 60", advanced)
	}
}

// TestPausedServerResumesWhenAPlayerJoins checks the world ticks again once a
// player is in. The loop runs free: a held loop spins its ticks as fast as the
// machine goes while the join is still on the wire, and the 30 seconds of loop
// time that is outlast the joiner's keep-alive before it has read a packet.
func TestPausedServerResumesWhenAPlayerJoins(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.PauseWhenEmptySeconds = 1
	srv, connect := servertest.Start(t, cfg)
	servertest.RunTicks(t, srv, 40)
	paused, _ := srv.Time()
	servertest.RunTicks(t, srv, 10)
	if again, _ := srv.Time(); again != paused {
		t.Fatalf("world age moved from %d to %d while paused", paused, again)
	}

	c := connect()
	c.Login("Sleeper")
	c.Configure()
	c.SkipJoin()
	before, _ := srv.Time()
	servertest.RunTicks(t, srv, 5)
	after, _ := srv.Time()
	if after-before < 5 {
		t.Errorf("world age advanced %d ticks in 5 full ticks after a player joined, want at least 5", after-before)
	}
}
