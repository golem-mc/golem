package commands

import (
	"bytes"
	"github.com/golem-mc/golem/internal/server/servertest"
	"slices"
	"strings"
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

// TestTimeSetMarkerMovesForward checks that /time set <marker> moves the
// clock forward to the marker's next occurrence rather than resetting it to
// an absolute total: night (13000) from 30000 lands on 37000, and a clock
// already standing on the marker moves one whole period more.
// vanilla: ClockTimeMarker.resolveTimeToMoveTo
func TestTimeSetMarkerMovesForward(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false

	srv, c := servertest.Join(t, cfg, "Testificate", servertest.OpsListing(t, "Testificate"), func(s *server.Server) {
		s.SetTimeSync(0)
		s.SetPaused(true)
	})

	c.ChatCommand("time set 30000")
	c.ExpectTimeFeedback("commands.time.set.absolute", textArg("minecraft:overworld"), intArg(30000))
	c.ExpectUpdateClocks(servertest.UpdateClock{Id: overworldClockID, Total: 30000, Rate: 0})

	// 30000 is 6000 of the day; the next 13000 stands 7000 ahead.
	c.ChatCommand("time set night")
	c.ExpectTimeFeedback("commands.time.set.time_marker", textArg("minecraft:overworld"), textArg("minecraft:night"))
	c.ExpectUpdateClocks(servertest.UpdateClock{Id: overworldClockID, Total: 37000, Rate: 0})
	if _, dayTime := srv.Time(); dayTime != 37000 {
		t.Errorf("after time set night = %d, want 37000", dayTime)
	}

	// noon (6000) from 37000 (13000 of the day) wraps to the next day.
	c.ChatCommand("time set noon")
	c.ExpectTimeFeedback("commands.time.set.time_marker", textArg("minecraft:overworld"), textArg("minecraft:noon"))
	c.ExpectUpdateClocks(servertest.UpdateClock{Id: overworldClockID, Total: 54000, Rate: 0})
	if _, dayTime := srv.Time(); dayTime != 54000 {
		t.Errorf("after time set noon = %d, want 54000", dayTime)
	}

	// 54000 is the noon the jump landed on, so one more noon is refused and
	// the clock stays.
	// vanilla: TimeCommand.setTimeToTimeMarker (NOT_MOVED)
	c.ChatCommand("time set noon")
	servertest.AwaitCommandErrorText(t, c, "commands.time.set.already_at_time_marker",
		textArg("minecraft:overworld"), textArg("minecraft:noon"))
	if _, dayTime := srv.Time(); dayTime != 54000 {
		t.Errorf("after time set noon on noon = %d, want 54000", dayTime)
	}

	c.Disconnect()
}

// TestTimeSetArgumentValues checks the tick counts ParseTimeArg reads,
// including its float rounding and int saturation.
// vanilla: TimeArgument.parse
func TestTimeSetArgumentValues(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false

	srv, c := servertest.Join(t, cfg, "Testificate", servertest.OpsListing(t, "Testificate"), func(s *server.Server) {
		s.SetTimeSync(0)
		s.SetPaused(true)
	})

	for _, tc := range []struct {
		name  string
		value string
		want  int64
	}{
		{"half rounds up", "2.5", 3},
		{"half a tick rounds up", "0.5t", 1},
		{"a day count saturates", "100000d", 2147483647},
		// Between the two saturations, so the second is not a refused no-op.
		{"the number parses as a Java float", "16777217", 16777216},
		{"an enormous count saturates", strings.Repeat("9", 40), 2147483647},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c.ChatCommand("time set " + tc.value)
			c.ExpectTimeFeedback("commands.time.set.absolute", textArg("minecraft:overworld"), intArg(tc.want))
			c.ExpectUpdateClocks(servertest.UpdateClock{Id: overworldClockID, Total: tc.want, Rate: 0})
			if _, dayTime := srv.Time(); dayTime != tc.want {
				t.Errorf("time set %s = %d, want %d", tc.value, dayTime, tc.want)
			}
		})
	}

	for _, tc := range []struct {
		name  string
		value string
		total int64
	}{
		{"half rounds up", "2.5", 3},
		{"saturating", "2147483648", 2147483647},
		{"below zero clamps the total", "-1000", 0},
		{"negative days clamp the total", "-5d", 0},
	} {
		t.Run("add "+tc.name, func(t *testing.T) {
			// Set directly: a /time set 0 on a clock at 0 is refused.
			srv.SetDayTime(0)
			c.ChatCommand("time add " + tc.value)
			// vanilla: TimeCommand.addTime passes the new total as a long,
			// where set passes its own int argument.
			c.ExpectTimeFeedback("commands.time.set.absolute", textArg("minecraft:overworld"), longArg(tc.total))
			c.ExpectUpdateClocks(servertest.UpdateClock{Id: overworldClockID, Total: tc.total, Rate: 0})
			if _, dayTime := srv.Time(); dayTime != tc.total {
				t.Errorf("time add %s = %d, want %d", tc.value, dayTime, tc.total)
			}
		})
	}

	c.Disconnect()
}

// TestTimeAddWithUnits checks that the minecraft:time argument parser handles
// the d (days), s (seconds), and t (ticks) units.
func TestTimeAddWithUnits(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false

	srv, c := servertest.Join(t, cfg, "Testificate", servertest.OpsListing(t, "Testificate"), func(s *server.Server) { s.SetTimeSync(0) })

	// Pause the clock so the tick loop does not advance the time between
	// commands, which would make the expected totals wrong.
	c.ChatCommand("time pause")
	c.ExpectTimeFeedback("commands.time.pause", textArg("minecraft:overworld"))
	c.ExpectUpdateTimeWithRate(0, 0)

	for _, tc := range []struct {
		name string
		arg  string
		want int64
	}{
		{"ticks", "100", 100},
		{"ticks with t", "100t", 100},
		{"seconds", "5s", 100},
		{"half day", "0.5d", 12000},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Set directly: a /time set 0 on a clock at 0 is refused.
			srv.SetDayTime(0)
			c.ChatCommand("time add " + tc.arg)
			c.ExpectTimeFeedback("commands.time.set.absolute", textArg("minecraft:overworld"), longArg(tc.want))
			c.ExpectUpdateTime(tc.want)
		})
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

// TestTimeOfTheEndClock tests that /time of the_end answers with the_end
// clock's own total, set and queried by the commands run against it, while
// the overworld's clock keeps its own. Both clocks are paused in the setup,
// so the totals the commands leave are the totals the assertions read, and
// the broadcast a set sends carries the_end's entry alone.
func TestTimeOfTheEndClock(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false

	srv, c := servertest.Join(t, cfg, "Testificate", servertest.OpsListing(t, "Testificate"), func(s *server.Server) {
		s.SetTimeSync(0)
		s.SetPaused(true)
		s.SetClockPaused("minecraft:the_end", true)
	})

	c.ChatCommand("time set 986")
	c.ExpectTimeFeedback("commands.time.set.absolute", textArg("minecraft:overworld"), intArg(986))
	c.ExpectUpdateClocks(servertest.UpdateClock{Id: overworldClockID, Total: 986, Rate: 0})

	// The clock is named as the command typed it, but the feedback
	// carries the resolved id.
	c.ChatCommand("time of the_end set 4502")
	c.ExpectTimeFeedback("commands.time.set.absolute", textArg("minecraft:the_end"), intArg(4502))
	// The broadcast carries the_end's clock alone, stopped at 4502 and
	// still paused, not the overworld's entry.
	c.ExpectUpdateClocks(servertest.UpdateClock{Id: endClockID, Total: 4502, Rate: 0})

	c.ChatCommand("time of the_end query time")
	c.ExpectTimeFeedback("commands.time.query.absolute", textArg("minecraft:the_end"), longArg(4502))

	// The two clocks keep their own totals.
	if _, dayTime := srv.Time(); dayTime != 986 {
		t.Errorf("dayTime = %d, want 986", dayTime)
	}
	if got := srv.EndClockTime(); got != 4502 {
		t.Errorf("the_end clock = %d, want 4502", got)
	}

	// The bare name and the full id reach the same clock.
	c.ChatCommand("time of minecraft:the_end query time")
	c.ExpectTimeFeedback("commands.time.query.absolute", textArg("minecraft:the_end"), longArg(4502))

	c.Disconnect()
}

// TestTimeOfTheEndClockBroadcasts checks that every /time of the_end command
// that changes the clock — set, add, pause, resume and rate — answers with
// an Update Time whose one entry is the_end's clock, at the total and the
// rate the command left it with. The tick loop is stopped, so the totals do
// not move between the command and the packet.
func TestTimeOfTheEndClockBroadcasts(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false

	srv, c := servertest.Dial(t, cfg, servertest.OpsListing(t, "Testificate"), func(s *server.Server) {
		s.SetTimeSync(0)
	})
	srv.StopTick()
	c.Join("Testificate")

	c.ChatCommand("time of the_end set 4502")
	c.ExpectTimeFeedback("commands.time.set.absolute", textArg("minecraft:the_end"), intArg(4502))
	c.ExpectUpdateClocks(servertest.UpdateClock{Id: endClockID, Total: 4502, Rate: 1})

	c.ChatCommand("time of the_end add 100")
	c.ExpectTimeFeedback("commands.time.set.absolute", textArg("minecraft:the_end"), longArg(4602))
	c.ExpectUpdateClocks(servertest.UpdateClock{Id: endClockID, Total: 4602, Rate: 1})

	c.ChatCommand("time of the_end pause")
	c.ExpectTimeFeedback("commands.time.pause", textArg("minecraft:the_end"))
	c.ExpectUpdateClocks(servertest.UpdateClock{Id: endClockID, Total: 4602, Rate: 0})

	c.ChatCommand("time of the_end resume")
	c.ExpectTimeFeedback("commands.time.resume", textArg("minecraft:the_end"))
	c.ExpectUpdateClocks(servertest.UpdateClock{Id: endClockID, Total: 4602, Rate: 1})

	c.ChatCommand("time of the_end rate 2")
	c.ExpectTimeFeedback("commands.time.rate", textArg("minecraft:the_end"), floatArg(2))
	c.ExpectUpdateClocks(servertest.UpdateClock{Id: endClockID, Total: 4602, Rate: 2})

	c.Disconnect()
}

// TestTimeOfTheEndSetMarkerRefused checks that /time of the_end set <marker>
// fails with the vanilla refusal: every marker belongs to the day timeline,
// which rides the overworld's clock, so no marker is found for the_end's and
// the error names the marker's resource key and the clock asked for — the
// same naming for a word that is not a marker at all.
func TestTimeOfTheEndSetMarkerRefused(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false

	_, c := servertest.Join(t, cfg, "Testificate", servertest.OpsListing(t, "Testificate"), func(s *server.Server) {
		s.SetTimeSync(0)
	})

	for marker, name := range map[string]string{
		"day":      "minecraft:day",
		"nonsense": "minecraft:nonsense",
	} {
		command := "time of the_end set " + marker
		c.ChatCommand(command)
		want := servertest.CommandErrorMessageForTest("commands.time.no_time_marker_found", name, "minecraft:the_end")
		data := c.ExpectPlay(protocol.IDPlaySystemChat)
		if !bytes.Equal(data, want) {
			c.T.Errorf("%s refusal = % X, want % X", command, data, want)
		}
	}

	c.Disconnect()
}

// TestTimeInDimensionUsesDefaultClock checks that a bare /time without /time
// of turns the dimension the command runs in: the end's clock answers in the
// end, and the nether, whose time is fixed, has no default clock and fails
// with the vanilla refusal naming it.
func TestTimeInDimensionUsesDefaultClock(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false

	srv, c := servertest.Dial(t, cfg, servertest.OpsListing(t, "Testificate"), func(s *server.Server) {
		s.SetTimeSync(0)
	})
	srv.StopTick()
	c.Join("Testificate")

	c.ChatCommand("time of the_end set 4502")
	c.ExpectTimeFeedback("commands.time.set.absolute", textArg("minecraft:the_end"), intArg(4502))

	c.ChatCommand("execute in minecraft:the_end run time query time")
	c.ExpectTimeFeedback("commands.time.query.absolute", textArg("minecraft:the_end"), longArg(4502))

	c.ChatCommand("execute in minecraft:the_nether run time query time")
	want := servertest.CommandErrorMessageForTest("commands.time.no_default_clock", "minecraft:the_nether")
	data := c.ExpectPlay(protocol.IDPlaySystemChat)
	if !bytes.Equal(data, want) {
		c.T.Errorf("no default clock refusal = % X, want % X", data, want)
	}

	c.Disconnect()
}

// TestTimeQueryGametimeInClocklessDimensions checks that query gametime needs
// no clock, while query time still refuses.
func TestTimeQueryGametimeInClocklessDimensions(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false

	srv, c := servertest.Dial(t, cfg, servertest.OpsListing(t, "Testificate"), func(s *server.Server) {
		s.SetTimeSync(0)
	})
	srv.StopTick()
	c.Join("Testificate")

	worldAge, _ := srv.Time()
	for _, dim := range []string{"minecraft:the_nether", "minecraft:the_end"} {
		c.ChatCommand("execute in " + dim + " run time query gametime")
		c.ExpectTimeFeedback("commands.time.query.gametime", longArg(worldAge))
	}

	c.ChatCommand("execute in minecraft:the_nether run time query time")
	want := servertest.CommandErrorMessageForTest("commands.time.no_default_clock", "minecraft:the_nether")
	if data := c.ExpectPlay(protocol.IDPlaySystemChat); !bytes.Equal(data, want) {
		c.T.Errorf("no default clock refusal = % X, want % X", data, want)
	}

	c.Disconnect()
}

// TestGameruleChangeBroadcastsBothClocks checks that a change of the
// advance_time game rule re-tells every player both clocks — the sync the
// rule change owes them — with the rule off reading as the rate 0 a frozen
// clock carries, and the rule back on restoring each clock's rate. The tick
// loop is stopped before the commands, but a tick or two may have run
// between the loop's start and the stop, so the totals the packets must
// carry are read from the server, which the stopped loop holds still.
func TestGameruleChangeBroadcastsBothClocks(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false

	srv, c := servertest.Dial(t, cfg, servertest.OpsListing(t, "Testificate"), func(s *server.Server) {
		s.SetTimeSync(0)
	})
	srv.StopTick()
	c.Join("Testificate")

	_, overworld := srv.Time()
	end := srv.EndClockTime()

	c.ChatCommand("gamerule advance_time false")
	c.ExpectTimeFeedback("commands.gamerule.set", textArg("advance_time"), textArg("false"))
	c.ExpectUpdateClocks(
		servertest.UpdateClock{Id: overworldClockID, Total: overworld, Rate: 0},
		servertest.UpdateClock{Id: endClockID, Total: end, Rate: 0},
	)

	c.ChatCommand("gamerule advance_time true")
	c.ExpectTimeFeedback("commands.gamerule.set", textArg("advance_time"), textArg("true"))
	c.ExpectUpdateClocks(
		servertest.UpdateClock{Id: overworldClockID, Total: overworld, Rate: 1},
		servertest.UpdateClock{Id: endClockID, Total: end, Rate: 1},
	)

	c.Disconnect()
}

// TestTimeOfUnknownClock is refused at parse time for names the
// minecraft:world_clock registry does not hold: the resource error, then the
// context line.
func TestTimeOfUnknownClock(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false

	_, c := servertest.Join(t, cfg, "Testificate", servertest.OpsListing(t, "Testificate"))

	command := "time of minecraft:nonsense query time"
	c.ChatCommand(command)
	// The cursor stands where the clock argument ends: the parser stopped
	// once the resource it read turned out not to be in the registry.
	c.ExpectCommandError(
		servertest.CommandErrorMessageForTest("argument.resource.not_found", "minecraft:nonsense", "minecraft:world_clock"),
		servertest.CommandErrorContextForTest(command, len("time of minecraft:nonsense")),
	)

	c.Disconnect()
}

// TestTimeOfWrongTimeline checks that a timeline asked of the clock it does
// not ride fails with wrong_timeline_for_clock naming the timeline and then
// the clock, alone rather than followed by a context line: the walk resolves
// the command, and the failure is the command's own.
func TestTimeOfWrongTimeline(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false

	_, c := servertest.Join(t, cfg, "Testificate", servertest.OpsListing(t, "Testificate"), func(s *server.Server) {
		s.SetTimeSync(0)
		s.SetPaused(true)
	})

	c.ChatCommand("time of the_end query day")
	data := c.ExpectPlay(protocol.IDPlaySystemChat)
	if want := servertest.CommandErrorMessageForTest("commands.time.wrong_timeline_for_clock", "minecraft:day", "minecraft:the_end"); !bytes.Equal(data, want) {
		c.T.Errorf("wrong timeline error = % X, want % X", data, want)
	}

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

// TestTimeSetAboveDay checks that setting a time above 24000 stores the total
// ticks unwrapped, the way the vanilla server's setTotalTicks does — the client
// wraps at a full day for display.
func TestTimeSetAboveDay(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false

	srv, c := servertest.Join(t, cfg, "Testificate", servertest.OpsListing(t, "Testificate"), func(s *server.Server) {
		s.SetTimeSync(0)
		s.SetPaused(true)
	})

	c.ChatCommand("time set 25000")
	c.ExpectTimeFeedback("commands.time.set.absolute", textArg("minecraft:overworld"), intArg(25000))

	if _, dayTime := srv.Time(); dayTime != 25000 {
		t.Errorf("dayTime = %d, want 25000 (unwrapped)", dayTime)
	}

	// /time query time returns the total ticks, not the wrapped value.
	c.ChatCommand("time query time")
	c.ExpectTimeFeedback("commands.time.query.absolute", textArg("minecraft:overworld"), longArg(25000))

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

// TestTimeQueryResultIsTheTime checks that /execute store result takes the
// time /time reports, wrapped at the int's range.
// vanilla: TimeCommand.queryTime, TimeCommand.wrapTime
func TestTimeQueryResultIsTheTime(t *testing.T) {
	t.Parallel()
	srv, c := joinOpForStore(t)
	c.ChatCommand("time set 1234")
	expectStored(t, srv, c, [][2]string{
		{"time query time", "1234"},
		{"time add 100", "1334"},
		{"time set 2147483520", "2147483520"},
		{"time add 1000", "873"}, // 2147484520 % 2147483647
		{"time pause", "1"},
	})
}

// TestTimePauseTwiceIsRefused checks the /time no-ops that throw: a pause of
// a paused clock, a resume of a running one, a rate or a total the clock
// already has. Each is refused naming the clock, and stores no success.
// vanilla: TimeCommand.setPaused, setRate, setTotalTicks
func TestTimePauseTwiceIsRefused(t *testing.T) {
	t.Parallel()
	srv, c := joinOpForStore(t)
	overworld := textArg("minecraft:overworld")

	c.ChatCommand("time pause")
	c.ExpectTimeFeedback("commands.time.pause", overworld)
	c.ChatCommand("time pause")
	servertest.AwaitCommandErrorText(t, c, "commands.time.pause.already_paused", overworld)
	if got := storedSuccess(t, srv, c, "time pause"); got != "0" {
		t.Errorf("a second pause stored success %s, want 0", got)
	}

	c.ChatCommand("time of minecraft:the_end resume")
	servertest.AwaitCommandErrorText(t, c, "commands.time.pause.already_running", textArg("minecraft:the_end"))

	c.ChatCommand("time rate 1")
	servertest.AwaitCommandErrorText(t, c, "commands.time.rate.already_same", overworld, floatArg(1))

	c.ChatCommand("time set 5000")
	c.ExpectTimeFeedback("commands.time.set.absolute", overworld, intArg(5000))
	c.ChatCommand("time set 5000")
	servertest.AwaitCommandErrorText(t, c, "commands.time.set.already_at_time", overworld, intArg(5000))
	if _, total := srv.Time(); total != 5000 {
		t.Errorf("the clock after the refused set = %d, want 5000", total)
	}
}
