package sleep

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"testing"

	"github.com/golem-mc/golem/nbt"
)

// sleepTicksAfterWake mirrors the server's own constant, the count a wake's
// residue climbs to before it resets to nothing.
const sleepTicksAfterWake = 110

// TestTheSleepCounterIsSavedWhileTheBodyLiesInBed puts a player to sleep,
// drives the count up to sixty and saves with the body still lying there, the
// snapshot an auto-save takes, and checks the file carries the count under the
// name the game stores it.
func TestTheSleepCounterIsSavedWhileTheBodyLiesInBed(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	srv, c := servertest.Join(t, cfg, "Actor", servertest.NoTimeSync)

	srv.StopTick()
	c.ExpectSleepStarted(srv, 6.5, 0.5, 0)

	for range 60 {
		srv.TickSleeping()
	}
	if got := srv.SleepTicks("Actor"); got != 60 {
		t.Fatalf("the sleeper's count after sixty ticks is %d, want 60", got)
	}

	srv.SaveAll(false)

	root := servertest.ReadPlayerFile(t, servertest.PlayerFile(cfg.LevelName, "Actor"))
	timer, ok := nbt.Get[nbt.Short](root, "SleepTimer")
	if !ok {
		t.Fatal("the file carries no SleepTimer")
	}
	if timer != 60 {
		t.Errorf("SleepTimer = %d, want the sleeper's 60", timer)
	}
}
