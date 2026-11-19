package sleep

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"testing"

	"github.com/golem-mc/golem/internal/server/world"
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

// TestTheSleepCounterIsRestoredOnRejoining writes a player file carrying a
// sleep count, the way a body saved mid-count leaves one, and checks the next
// connection picks the count up and goes on climbing it.
func TestTheSleepCounterIsRestoredOnRejoining(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	cfg.LevelName = t.TempDir()
	servertest.WritePlayerFile(t, servertest.PlayerFile(cfg.LevelName, "Actor"), nbt.Compound{
		"SleepTimer": nbt.Short(60),
	})

	srv, connect := servertest.Start(t, cfg, servertest.NoTimeSync)
	srv.StopTick()
	connect().Join("Actor")

	if got := srv.SleepTimer("Actor"); got != 60 {
		t.Fatalf("the rejoined body's sleep count is %d, want the file's 60", got)
	}

	// The count a loaded body carries is the residue a wake left: it goes
	// on climbing to the hundred and ten it resets at.
	for range 49 {
		srv.TickSleeping()
	}
	if got := srv.SleepTimer("Actor"); got != 109 {
		t.Fatalf("the count is %d after the climb, want 109", got)
	}
	srv.TickSleeping()
	if got := srv.SleepTimer("Actor"); got != 0 {
		t.Errorf("the count is %d at the hundred and ten, want 0", got)
	}
}

// TestTheNightsPassingLeavesTheCounterClimbing checks the count the night's
// skip leaves on the sleeper it wakes: the hundred the wake writes, then the
// climb the same tick's counter runs, so the body it stands up carries a
// hundred and one.
func TestTheNightsPassingLeavesTheCounterClimbing(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Actor", servertest.NoTimeSync)

	srv.StopTick()
	c.ExpectSleepStarted(srv, 6.5, 0.5, 0)

	for range sleepTicksToSkip + 1 {
		srv.TickSleeping()
	}
	if srv.Sleeping("Actor") {
		t.Fatal("the night passed and left the sleeper lying")
	}
	if got := srv.SleepTimer("Actor"); got != 101 {
		t.Errorf("the night's passing left the count at %d, want 101", got)
	}
}

// TestTheDawnLeavesTheCounterClimbing checks the count the dawn's wake leaves
// and the climb after it: the hundred the wake writes that tick, then a
// hundred and one up to the hundred and nine, and nought at the hundred and
// ten.
func TestTheDawnLeavesTheCounterClimbing(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Actor", servertest.NoTimeSync)

	srv.StopTick()
	c.ExpectSleepStarted(srv, 6.5, 0.5, 0)

	srv.SetDayTime(testNoon)
	srv.TickSleeping()
	if srv.Sleeping("Actor") {
		t.Fatal("the dawn left the sleeper lying")
	}
	if got := srv.SleepTimer("Actor"); got != 100 {
		t.Fatalf("the dawn's wake left the count at %d, want 100", got)
	}

	for want := 101; want < sleepTicksAfterWake; want++ {
		srv.TickSleeping()
		if got := srv.SleepTimer("Actor"); got != want {
			t.Fatalf("the count is %d, want %d", got, want)
		}
	}
	srv.TickSleeping()
	if got := srv.SleepTimer("Actor"); got != 0 {
		t.Errorf("the count is %d at the hundred and ten, want 0", got)
	}
}

// TestTheCounterClimbsInEveryDimension wakes a sleeper at the dawn, carries
// the body to each other dimension while the count is climbing and checks the
// climb goes on there: the residue belongs to the body, not the world, so a
// body that changes worlds keeps counting.
func TestTheCounterClimbsInEveryDimension(t *testing.T) {
	t.Parallel()
	for _, dimension := range []string{world.NetherName, world.EndName} {
		srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Actor", servertest.NoTimeSync)

		srv.StopTick()
		c.ExpectSleepStarted(srv, 6.5, 0.5, 0)

		srv.SetDayTime(testNoon)
		srv.TickSleeping()
		if got := srv.SleepTimer("Actor"); got != 100 {
			t.Fatalf("the dawn's wake left the count at %d, want 100", got)
		}

		srv.MovePlayerToDimension("Actor", dimension, 8.5, 64, 8.5)
		if got := srv.PlayerDimension("Actor"); got != dimension {
			t.Fatalf("the body is in %q, want %q", got, dimension)
		}

		srv.TickSleeping()
		if got := srv.SleepTimer("Actor"); got != 101 {
			t.Errorf("the count in %s is %d after a tick, want 101", dimension, got)
		}
	}
}
