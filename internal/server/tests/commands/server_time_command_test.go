package commands

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"slices"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/registry"
)

// The registry IDs of the two clocks the minecraft:world_clock registry
// numbers, what an Update Time entry names its clock by.
var (
	overworldClockID = registry.MustID("minecraft:world_clock", "minecraft:overworld")
	endClockID       = registry.MustID("minecraft:world_clock", "minecraft:the_end")
)

// textArg, intArg, longArg and floatArg are the shapes a /time feedback
// argument travels as: the clock, marker and timeline names as plain text,
// the value /time set ran to as an int, the total an add and the queries
// answer as a long and the rate as a float.
func textArg(s string) protocol.Text { return protocol.Text{Text: s} }

func intArg(v int64) protocol.Text { return protocol.Text{Number: &v} }

func longArg(v int64) protocol.Text { return protocol.Text{Long: &v} }

func floatArg(v float32) protocol.Text { return protocol.Text{Float: &v} }

// TestTimeSetNumeric sets the time of day to a numeric value and checks that
// the feedback message matches vanilla's "Set minecraft:overworld to <n> tick(s)"
// and that the time is broadcast to all players.
func TestTimeSetNumeric(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false

	srv, c := servertest.Join(t, cfg, "Testificate", servertest.OpsListing(t, "Testificate"), func(s *server.Server) {
		s.SetTimeSync(0)  // disable periodic time sync so it doesn't interfere
		s.SetPaused(true) // freeze the clock so the tick loop does not advance it
	})

	c.ChatCommand("time set 1000")

	// The player gets the feedback message.
	c.ExpectTimeFeedback("commands.time.set.absolute", textArg("minecraft:overworld"), intArg(1000))
	// The player also gets the broadcast Update Time packet.
	c.ExpectUpdateTime(1000)

	// The world's time was actually changed.
	if _, dayTime := srv.Time(); dayTime != 1000 {
		t.Errorf("dayTime = %d, want 1000", dayTime)
	}

	c.Disconnect()
}

// TestTimeSetMarker sets the time to a named marker like "noon" and checks the
// feedback message.
func TestTimeSetMarker(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false

	srv, c := servertest.Join(t, cfg, "Testificate", servertest.OpsListing(t, "Testificate"), func(s *server.Server) {
		s.SetTimeSync(0)
		s.SetPaused(true)
	})

	c.ChatCommand("time set noon")
	c.ExpectTimeFeedback("commands.time.set.time_marker", textArg("minecraft:overworld"), textArg("minecraft:noon"))
	c.ExpectUpdateTime(6000)

	if _, dayTime := srv.Time(); dayTime != 6000 {
		t.Errorf("dayTime = %d, want 6000", dayTime)
	}

	c.Disconnect()
}

// TestTimeAdd adds to the time of day and checks the feedback reports the new
// total.
func TestTimeAdd(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false

	srv, c := servertest.Join(t, cfg, "Testificate", servertest.OpsListing(t, "Testificate"), func(s *server.Server) {
		s.SetTimeSync(0)  // disable periodic time sync so it doesn't interfere
		s.SetPaused(true) // freeze the clock so the tick loop does not advance it
	})

	// Add 500 ticks.
	c.ChatCommand("time set 1000")
	c.ExpectTimeFeedback("commands.time.set.absolute", textArg("minecraft:overworld"), intArg(1000))
	c.ExpectUpdateTime(1000)

	c.ChatCommand("time add 500")
	// vanilla: TimeCommand.addTime passes the new total as a long, where
	// set passes its own int argument.
	c.ExpectTimeFeedback("commands.time.set.absolute", textArg("minecraft:overworld"), longArg(1500))
	c.ExpectUpdateTime(1500)

	if _, dayTime := srv.Time(); dayTime != 1500 {
		t.Errorf("dayTime = %d, want 1500", dayTime)
	}

	c.Disconnect()
}

// TestTimeQueryGametime queries the game time (world age).
func TestTimeQueryGametime(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false

	srv, c := servertest.Dial(t, cfg, servertest.OpsListing(t, "Testificate"), func(s *server.Server) {
		s.SetTimeSync(0)
		s.SetPaused(true)
	})
	// Stop the tick loop so the world age stops advancing. SetPaused only
	// freezes the day time; the world age increments every tick regardless,
	// which would race the exact value the query reports.
	srv.StopTick()
	c.Join("Testificate")

	c.ChatCommand("time query gametime")
	worldAge, _ := srv.Time()
	c.ExpectTimeFeedback("commands.time.query.gametime", longArg(worldAge))

	c.Disconnect()
}

// TestTimeQueryTime queries the time of day.
func TestTimeQueryTime(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false

	_, c := servertest.Join(t, cfg, "Testificate", servertest.OpsListing(t, "Testificate"), func(s *server.Server) {
		s.SetTimeSync(0)
		s.SetPaused(true)
	})

	c.ChatCommand("time set 6000")
	c.ExpectTimeFeedback("commands.time.set.absolute", textArg("minecraft:overworld"), intArg(6000))
	c.ExpectUpdateTime(6000)

	c.ChatCommand("time query time")
	c.ExpectTimeFeedback("commands.time.query.absolute", textArg("minecraft:overworld"), longArg(6000))

	c.Disconnect()
}

// TestTimePauseResume checks that pausing the clock stops it advancing and
// resuming starts it again.
func TestTimePauseResume(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false

	srv, c := servertest.Join(t, cfg, "Testificate", servertest.OpsListing(t, "Testificate"), func(s *server.Server) { s.SetTimeSync(0) })

	c.ChatCommand("time set 1000")
	c.ExpectTimeFeedback("commands.time.set.absolute", textArg("minecraft:overworld"), intArg(1000))
	c.ExpectUpdateTime(1000)

	c.ChatCommand("time pause")
	c.ExpectTimeFeedback("commands.time.pause", textArg("minecraft:overworld"))
	// The broadcast after pause carries rate 0.
	c.ExpectUpdateTimeWithRate(1000, 0)

	if !srv.IsPaused() {
		t.Error("expected clock to be paused")
	}

	c.ChatCommand("time resume")
	c.ExpectTimeFeedback("commands.time.resume", textArg("minecraft:overworld"))
	c.ExpectUpdateTimeWithRate(1000, 1)

	if srv.IsPaused() {
		t.Error("expected clock to not be paused")
	}

	c.Disconnect()
}

// TestTimeRate sets the clock rate and checks the feedback.
func TestTimeRate(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false

	srv, c := servertest.Join(t, cfg, "Testificate", servertest.OpsListing(t, "Testificate"), func(s *server.Server) { s.SetTimeSync(0) })

	// Pause, then resume, so the clock is running but the tick loop has not
	// advanced the time much.
	c.ChatCommand("time pause")
	c.ExpectTimeFeedback("commands.time.pause", textArg("minecraft:overworld"))
	c.ExpectUpdateTimeWithRate(0, 0)

	c.ChatCommand("time resume")
	c.ExpectTimeFeedback("commands.time.resume", textArg("minecraft:overworld"))
	c.ExpectUpdateTimeWithRate(0, 1)

	c.ChatCommand("time rate 2")
	// The rate the feedback carries renders the way a Java float renders:
	// always with a decimal point.
	c.ExpectTimeFeedback("commands.time.rate", textArg("minecraft:overworld"), floatArg(2))
	c.ExpectUpdateTimeWithRate(0, 2)

	if rate := srv.GetRate(); rate != 2 {
		t.Errorf("rate = %v, want 2", rate)
	}

	c.Disconnect()
}

// TestTimeOfClock tests that /time of <clock> works the same as /time for the
// overworld clock.
func TestTimeOfClock(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false

	_, c := servertest.Join(t, cfg, "Testificate", servertest.OpsListing(t, "Testificate"), func(s *server.Server) {
		s.SetTimeSync(0)
		s.SetPaused(true)
	})

	c.ChatCommand("time of minecraft:overworld set 1000")
	c.ExpectTimeFeedback("commands.time.set.absolute", textArg("minecraft:overworld"), intArg(1000))
	c.ExpectUpdateTime(1000)

	c.Disconnect()
}

// TestTimeQueryTimelines queries the timelines the minecraft:timeline
// registry has: the remainder of the clock's total modulo each period, the
// period-less timeline's raw total, and the repetition counts.
func TestTimeQueryTimelines(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false

	_, c := servertest.Join(t, cfg, "Testificate", servertest.OpsListing(t, "Testificate"), func(s *server.Server) {
		s.SetTimeSync(0)
		s.SetPaused(true)
	})

	c.ChatCommand("time set 30000")
	c.ExpectTimeFeedback("commands.time.set.absolute", textArg("minecraft:overworld"), intArg(30000))

	c.ChatCommand("time query minecraft:moon")
	c.ExpectTimeFeedback("commands.time.query.timeline", textArg("minecraft:moon"), longArg(30000))

	c.ChatCommand("time query minecraft:early_game")
	c.ExpectTimeFeedback("commands.time.query.timeline", textArg("minecraft:early_game"), longArg(30000))

	c.ChatCommand("time query minecraft:villager_schedule")
	c.ExpectTimeFeedback("commands.time.query.timeline", textArg("minecraft:villager_schedule"), longArg(6000))

	c.ChatCommand("time query minecraft:villager_schedule repetition")
	c.ExpectTimeFeedback("commands.time.query.timeline.repetitions", textArg("minecraft:villager_schedule"), longArg(1))

	c.ChatCommand("time query minecraft:day repetition")
	c.ExpectTimeFeedback("commands.time.query.timeline.repetitions", textArg("minecraft:day"), longArg(1))

	c.ChatCommand("time query minecraft:moon repetition")
	c.ExpectTimeFeedback("commands.time.query.timeline.repetitions", textArg("minecraft:moon"), longArg(0))

	c.ChatCommand("time query minecraft:early_game repetition")
	c.ExpectTimeFeedback("commands.time.query.timeline.repetitions", textArg("minecraft:early_game"), longArg(0))

	c.Disconnect()
}

// TestTimeCommandSuggestions checks that the command tree includes the time
// command and its literals are suggested, which needs the runner to be an
// operator: a non-op's tree has no /time to suggest.
func TestTimeCommandSuggestions(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false

	_, c := servertest.Join(t, cfg, "Testificate", servertest.OpsListing(t, "Testificate"))

	for _, tc := range []struct {
		name    string
		text    string
		matches []string
	}{
		{"partial time", "/tim", []string{"time"}},
		{"time subcommands", "/time ", []string{"add", "of", "pause", "query", "rate", "resume", "set"}},
		// The markers the set argument offers are the day timeline's four,
		// the overworld clock's only.
		{"time set markers", "/time set ", []string{
			"minecraft:day", "minecraft:midnight", "minecraft:night", "minecraft:noon",
		}},
		// The partial matches the path behind the markers' namespace.
		{"time set marker paths", "/time set n", []string{"minecraft:night", "minecraft:noon"}},
		// The literals and the timelines the registry has, offered together.
		{"time query subcommands", "/time query ", []string{
			"gametime", "minecraft:day", "minecraft:early_game", "minecraft:moon", "minecraft:villager_schedule", "time",
		}},
		// A partial matches the timelines by their path as well as their
		// full name.
		{"time query partial", "/time query m", []string{
			"minecraft:day", "minecraft:early_game", "minecraft:moon", "minecraft:villager_schedule",
		}},
		{"time query path", "/time query d", []string{"minecraft:day"}},
		// The match is the vanilla one: the partial may sit after an
		// underscore of the entry's name.
		{"time query after underscore", "/time query sch", []string{"minecraft:villager_schedule"}},
		// The partial is case folded, like the vanilla resource match.
		{"time query capitals", "/time query MI", []string{
			"minecraft:day", "minecraft:early_game", "minecraft:moon", "minecraft:villager_schedule",
		}},
		// The clocks the world_clock registry has, matched after the
		// underscore of their path too.
		{"time of clocks", "/time of ", []string{"minecraft:overworld", "minecraft:the_end"}},
		{"time of clock path", "/time of end", []string{"minecraft:the_end"}},
		// The timelines offered answer for the clock /time of named —
		// all of them for the overworld's, none for the_end's, whose
		// timelines this server does not run.
		{"overworld query timelines", "/time of minecraft:overworld query ", []string{
			"minecraft:day", "minecraft:early_game", "minecraft:moon", "minecraft:villager_schedule", "time",
		}},
		{"the_end query timelines", "/time of the_end query ", []string{"time"}},
		// The markers /time of's clock is offered are that clock's own: the
		// overworld's four, and none for the_end, whose timelines this
		// server does not run.
		{"overworld set markers", "/time of minecraft:overworld set ", []string{
			"minecraft:day", "minecraft:midnight", "minecraft:night", "minecraft:noon",
		}},
		{"the_end set markers", "/time of the_end set ", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c.CommandSuggestion(1, tc.text)
			_, _, _, matches := c.ExpectCommandSuggestions()
			if !slicesSortedEqual(sortedStrings(matches), sortedStrings(tc.matches)) {
				t.Errorf("matches = %v, want %v", matches, tc.matches)
			}
		})
	}

	c.Disconnect()
}

// slicesSortedEqual reports whether two string slices are equal.
func slicesSortedEqual(a, b []string) bool {
	return slices.Equal(a, b)
}

// sortedStrings returns a sorted copy of s.
func sortedStrings(s []string) []string {
	out := slices.Clone(s)
	slices.Sort(out)
	return out
}
