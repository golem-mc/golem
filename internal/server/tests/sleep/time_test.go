package sleep

import (
	"testing"
	"time"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// TestTimeAdvancesInTickLoop checks that the main loop advances the world's
// clock by one tick per tick, the way the vanilla server's does. A server that
// has been up for a few ticks has a world age to show for it.
func TestTimeAdvancesInTickLoop(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.LevelName = t.TempDir()

	srv, _ := servertest.Start(t, cfg)

	age, _ := srv.Time()
	before := age
	// The loop ticks every 50ms; a quarter second is five ticks, which is
	// enough to see the clock move without making the test slow.
	servertest.PollUntil(t, 2*time.Second, func() bool {
		age, _ = srv.Time()
		return age > before+2
	})
}

// TestTimeSurvivesRestart checks that the world's clock is written to level.dat
// when the server shuts down, and read back when the next server opens the
// same world — so a restart picks up the day/night cycle where it was left off.
func TestTimeSurvivesRestart(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.LevelName = t.TempDir()

	srv, _ := servertest.Start(t, cfg)
	// Let the clock advance well past zero, so a reload that resets it to
	// the defaults is caught.
	servertest.PollUntil(t, 3*time.Second, func() bool {
		age, _ := srv.Time()
		return age > 10
	})
	age, _ := srv.Time()
	srv.Shutdown()

	// A server of its own, as after a restart, pointed at the same world.
	restarted := *cfg
	srv2, _ := servertest.Start(t, &restarted)
	defer srv2.Shutdown()
	againAge, _ := srv2.Time()
	// The clock the first server reached is a lower bound: the tick loop
	// may have advanced a few ticks between the read and the shutdown's
	// stop, but the clock never goes backwards.
	if againAge < age {
		t.Errorf("world age after restart = %d, want at least %d (the clock the first server reached)", againAge, age)
	}
}

// TestClientReceivesUpdateTimeOnJoin checks that the join sequence includes
// an Update Time packet carrying every clock, so a player arriving sees the
// sky and light the server has rather than the client's own defaults and
// learns about each clock in the first place.
func TestClientReceivesUpdateTimeOnJoin(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.LevelName = t.TempDir()

	// Advance the clock a little so the time the packet carries is not the
	// zero a freshly created world starts with, which would also be what a
	// missing field defaults to.
	srv, c := servertest.Dial(t, cfg)
	servertest.PollUntil(t, 2*time.Second, func() bool {
		age, _ := srv.Time()
		return age > 5
	})

	c.Login("Testificate")
	c.Configure()
	data := c.AwaitPlay(protocol.IDPlayUpdateTime)

	r := protocol.NewReader(data)
	worldAge := r.ReadI64()
	if got := r.ReadVarInt(); got != 2 {
		t.Fatalf("clocks = %d, want 2 (every registry clock)", got)
	}
	overworldID := registry.MustID("minecraft:world_clock", "minecraft:overworld")
	endID := registry.MustID("minecraft:world_clock", "minecraft:the_end")
	for _, want := range []struct {
		name string
		id   int32
	}{
		{"minecraft:overworld", overworldID},
		{"minecraft:the_end", endID},
	} {
		if got := r.ReadVarInt(); got != want.id {
			t.Errorf("%s entry carries clock ID %d, want %d", want.name, got, want.id)
		}
		dayTime := r.ReadVarLong()
		if got := r.ReadF32(); got != 0 {
			t.Errorf("%s entry fractional time = %g, want 0", want.name, got)
		}
		if rate := r.ReadF32(); rate != 1 {
			t.Errorf("%s entry rate = %g, want 1 (one clock tick per client tick)", want.name, rate)
		}
		// The world is fresh, so every clock has advanced as many ticks as
		// the world has stood.
		if dayTime != worldAge {
			t.Errorf("%s entry time = %d, want %d (the ticks since the world began)", want.name, dayTime, worldAge)
		}
	}
	if err := r.Err(); err != nil {
		t.Fatalf("update time: %v", err)
	}
	if r.Remaining() != 0 {
		t.Errorf("update time has %d bytes left over", r.Remaining())
	}
	// The packet was built during the join sequence, which ran after the
	// clock had already advanced past 5, so the world age it carries is at
	// least that.
	if worldAge < 5 {
		t.Errorf("world age = %d, want at least 5 (the clock had advanced before the join)", worldAge)
	}
}

// TestClientReceivesPeriodicUpdateTime checks that the main loop broadcasts
// the time to every player often enough to keep the client's clock from
// drifting, the way the vanilla server sends an Update Time packet
// periodically. The packet names no clock: the client advances each clock on
// its own at the rate a join or a /time command last carried, so this is a
// correction rather than the clock itself.
func TestClientReceivesPeriodicUpdateTime(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.LevelName = t.TempDir()

	srv, c := servertest.Join(t, cfg, "Testificate", func(s *server.Server) {
		// Sync every 100ms, the way the keep-alive test shortens its
		// interval, so a test does not have to wait out the real one.
		s.SetTimeSync(100 * time.Millisecond)
	})

	// The join sequence's Update Time has already arrived; the next one is
	// the periodic broadcast. Skip whatever the join sequence is still
	// sending and wait for a second Update Time.
	var updates int
	c.ReadTicks(srv, 100, func(packetID int32, data []byte) bool {
		switch packetID {
		case protocol.IDPlayUpdateTime:
			updates++
			r := protocol.NewReader(data)
			// The world age in the periodic update should be at least
			// as far along as the one the join update carried — the
			// clock only moves forward.
			if got := r.ReadI64(); got < 1 {
				t.Errorf("periodic world age = %d, want the clock to have advanced", got)
			}
			if got := r.ReadVarInt(); got != 0 {
				t.Errorf("periodic clock entries = %d, want 0", got)
			}
		}
		return updates >= 2
	})
	if updates < 2 {
		t.Errorf("received %d periodic Update Time packets, want at least 2", updates)
	}

	c.Disconnect()
	_ = srv
}

// TestPeriodicUpdateTimeSyncsEvery20Ticks checks that the main loop sends the
// world's age on vanilla's cadence - every 20 ticks, with an empty clock list
// - so the client's own clocks are corrected at the rate the vanilla server
// corrects them.
func TestPeriodicUpdateTimeSyncsEvery20Ticks(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.LevelName = t.TempDir()

	srv, c := servertest.Join(t, cfg, "Testificate")

	// The join sequence's Update Time was read with the join, so every one
	// from here is the periodic broadcast. Two fit inside 45 ticks, more
	// than twice the 20-tick interval.
	var ages []int64
	c.PumpTicks(srv, 45, func(id int32, data []byte) bool {
		if id != protocol.IDPlayUpdateTime {
			return false
		}
		r := protocol.NewReader(data)
		ages = append(ages, r.ReadI64())
		if got := r.ReadVarInt(); got != 0 {
			t.Errorf("periodic Update Time carries %d clocks, want 0", got)
		}
		return len(ages) >= 2
	})
	if len(ages) < 2 {
		t.Fatalf("read %d periodic Update Time packets in 45 ticks, want 2", len(ages))
	}
	if got := ages[1] - ages[0]; got != 20 {
		t.Errorf("periodic Update Time world ages differ by %d, want 20 (the vanilla cadence)", got)
	}
}
