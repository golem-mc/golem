package mobs

import (
	"github.com/golem-mc/golem/internal/server/servertest"

	"math"
	"testing"

	server "github.com/golem-mc/golem/internal/server"
)

// The phantom tests: the spawner that stands phantoms up over a player who
// has gone too long without sleeping, and the phantom's own flight. The loop
// is stopped; the spawner's passes are run by hand with a seed each, so a run
// is the same every time.

// insomniac is a rest count far past the 72000 ticks the spawner's roll
// starts to favour a phantom at.
const insomniac = 100_000_000

// phantomPasses bounds the passes a test throws at the spawner: its interval
// is 60 to 120 seconds, 1200 to 2400 ticks, and each interval's roll lands
// with the local difficulty's chance.
const phantomPasses = 60000

// phantomTests is the fixture: a survival player on the hard flat world's
// surface at midnight, the spawner turned on and the loop stopped.
func phantomTests(t *testing.T) (*server.Server, *servertest.Client) {
	t.Helper()
	srv, c := zombieTests(t, servertest.SpawningTestConfig())
	srv.SetDifficulty(3)
	srv.SetDayTime(midnight)
	srv.SetSpawning(true)
	return srv, c
}

// phantomsOf counts the phantoms in the world.
func phantomsOf(srv *server.Server) []server.TestMob {
	var found []server.TestMob
	for _, m := range srv.Mobs() {
		if m.Kind == "minecraft:phantom" {
			found = append(found, m)
		}
	}
	return found
}

// runPhantomPasses runs passes until a phantom stands or the budget is spent.
func runPhantomPasses(srv *server.Server) []server.TestMob {
	for seed := int64(1); seed <= phantomPasses; seed++ {
		srv.PhantomPass(seed)
		if found := phantomsOf(srv); len(found) > 0 {
			return found
		}
	}
	return nil
}

// TestSleeplessPlayerGetsPhantoms checks that a player under the open night
// sky whose rest count is far past 72000 has phantoms stood up 20 to 34
// blocks over them, within ten blocks sideways.
func TestSleeplessPlayerGetsPhantoms(t *testing.T) {
	t.Parallel()
	srv, _ := phantomTests(t)
	srv.SetTimeSinceRest("Watcher", insomniac)

	found := runPhantomPasses(srv)
	if len(found) == 0 {
		t.Fatal("no phantom stood up over a sleepless player in a night's worth of spawner intervals")
	}
	if len(found) > 4 {
		t.Errorf("%d phantoms in one group, want at most 1 + the hard difficulty's 3", len(found))
	}
	for _, m := range found {
		if m.Y < 64+20 || m.Y > 64+34 {
			t.Errorf("a phantom stands at y=%v, want 20 to 34 over the player's 64", m.Y)
		}
		if math.Abs(m.X-0.5) > 10.5 || math.Abs(m.Z-0.5) > 10.5 {
			t.Errorf("a phantom stands at (%v, %v), want within ten blocks of the player", m.X, m.Z)
		}
	}
}

// TestSleepingResetsTheRestCount puts a sleepless player to bed: the count
// goes to nothing, and the night after it brings no phantom.
func TestSleepingResetsTheRestCount(t *testing.T) {
	t.Parallel()
	srv, c := phantomTests(t)
	srv.StopTick()
	srv.SetTimeSinceRest("Watcher", insomniac)
	if found := runPhantomPasses(srv); len(found) == 0 {
		t.Fatal("the control: a sleepless player drew no phantom")
	}
	// Clear the control's phantoms away: the sleeper's night counts only
	// what is made after it.
	before := len(phantomsOf(srv))

	c.ExpectSleepStarted(srv, 6.5, 0.5, 0)
	if got := srv.TimeSinceRest("Watcher"); got != 0 {
		t.Fatalf("the rest count after lying down is %d, want 0", got)
	}
	// Standing again under the open sky at night.
	for seed := int64(1); seed <= phantomPasses; seed++ {
		srv.PhantomPass(seed)
	}
	if after := len(phantomsOf(srv)); after != before {
		t.Errorf("%d phantoms came after a sleep, want none", after-before)
	}
}

// TestNoPhantomsWithTheRuleOff checks that spawn_phantoms off keeps the
// spawner quiet over the sleepless.
func TestNoPhantomsWithTheRuleOff(t *testing.T) {
	t.Parallel()
	srv, _ := phantomTests(t)
	srv.SetTimeSinceRest("Watcher", insomniac)
	srv.SetGameRule("spawn_phantoms", false)

	if found := runPhantomPasses(srv); len(found) != 0 {
		t.Errorf("%d phantoms stood up with spawn_phantoms off", len(found))
	}
}

// TestPhantomBitesItsTarget checks the flight: a phantom put over a player
// at night finds them, circles, swoops and bites within a minute of ticks.
func TestPhantomBitesItsTarget(t *testing.T) {
	t.Parallel()
	srv, _ := phantomTests(t)
	srv.SetDayTime(midnight)
	srv.SpawnMob("minecraft:phantom", 0.5, 84, 0.5)

	was := srv.PlayerHealth("Watcher")
	bitten := false
	for range 120 {
		srv.TickMobs(10)
		if srv.PlayerHealth("Watcher") < was {
			bitten = true
			break
		}
	}
	if !bitten {
		t.Fatalf("the phantom never bit its target in 1200 ticks, the player is still at %v", was)
	}
	if got := was - srv.PlayerHealth("Watcher"); got != 3 {
		t.Errorf("default phantom bite on hard dealt %v health, want 3", got)
	}
}
