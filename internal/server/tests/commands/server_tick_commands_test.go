package commands

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/registry"
)

// The black-box side of the /tick command's rate, freeze, step and sprint
// subcommands: the translation keys a change answers with, the Set Ticking
// State and Step Tick packets every change and every join carry, and the
// pacing the rate and a sprint run the loop itself by.

// gateClock is a clock the test steps one wait at a time: each release lets
// the loop finish its current wait, and the clock jumps to whatever the
// loop asked for. The duration each released wait asked for is recorded,
// so the pacing the loop runs by is what the test reads.
type gateClock struct {
	mu      sync.Mutex
	now     time.Time
	release chan struct{}
	waits   []time.Duration
}

func newGateClock() *gateClock {
	return &gateClock{now: time.Unix(2_000_000, 0), release: make(chan struct{})}
}

func (c *gateClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

// Wait jumps the clock forward to t when t lies ahead, then blocks until
// the test releases the wait; the release is what makes a wait one of the
// recorded ones. It returns false once the loop is stopped.
func (c *gateClock) Wait(t time.Time, done <-chan struct{}) bool {
	c.mu.Lock()
	d := t.Sub(c.now)
	if d > 0 {
		c.now = c.now.Add(d)
	}
	c.mu.Unlock()
	select {
	case <-c.release:
		c.mu.Lock()
		c.waits = append(c.waits, d)
		c.mu.Unlock()
		return true
	case <-done:
		return false
	}
}

// step releases one wait.
func (c *gateClock) step() { c.release <- struct{}{} }

// paced reports the per-wait durations the loop asked for, in order.
func (c *gateClock) paced() []time.Duration {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]time.Duration(nil), c.waits...)
}

// assertPacing checks the per-wait pacing the loop asked for, waiting for
// the released waits to have recorded themselves first: a release returns
// before the loop has written its wait down.
func assertPacing(t *testing.T, clock *gateClock, want ...time.Duration) {
	t.Helper()
	servertest.PollUntil(t, 5*time.Second, func() bool { return len(clock.paced()) == len(want) })
	assertWaits(t, clock.paced(), want...)
}

// assertWaits checks the pacing the loop asked for wait by wait.
func assertWaits(t *testing.T, got []time.Duration, want ...time.Duration) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("the loop paced %d waits, want %d: %v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("wait %d paced %v, want %v", i, got[i], want[i])
		}
	}
}

// TestTickFreezeHoldsTheWorldAndTellsEveryone: freeze answers with the
// frozen status and tells every client the world is frozen; the world's
// age, its clocks and a hanging block the world would otherwise take out
// stand still while it is; and unfreeze lets them move again.
func TestTickFreezeHoldsTheWorldAndTellsEveryone(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	srv, c := servertest.Join(t, cfg, "Testificate", servertest.OpsListing(t, "Testificate"))

	c.ChatCommand("tick freeze")
	c.ExpectTickingState(20, true)
	c.ExpectCommandFeedback("commands.tick.status.frozen")

	// A tick in flight when the freeze landed may still advance the clock;
	// from one whole frozen tick to the next, nothing moves.
	servertest.AwaitTicks(t, srv, 5)
	age, day := srv.Time()
	end := srv.EndClockTime()

	// A hanging sand block holds where it stands: the block ticks and the
	// falling entity both are of the world's passes a freeze gates.
	sand, ok := registry.FindState("minecraft:sand", nil)
	if !ok {
		t.Fatal("the registry has no sand")
	}
	c.ChatCommand("setblock 3 70 3 sand")
	servertest.AwaitCommandFeedback(t, c, "commands.setblock.success", "3", "70", "3")
	c.AwaitBlockState(3, 70, 3, sand)

	servertest.AwaitTicks(t, srv, 20)
	frozen, day2 := srv.Time()
	if age != frozen || day != day2 {
		t.Errorf("the frozen world's clocks moved: age %d -> %d, clock %d -> %d", age, frozen, day, day2)
	}
	if got := srv.EndClockTime(); got != end {
		t.Errorf("the frozen end clock moved: %d -> %d", end, got)
	}
	if got := srv.BlockAt(3, 70, 3); got != sand {
		t.Errorf("the frozen world let a hanging block go: state %d, want %d", got, sand)
	}

	c.ChatCommand("tick unfreeze")
	c.ExpectTickingState(20, false)
	c.ExpectCommandFeedback("commands.tick.status.running")
	servertest.AwaitTicks(t, srv, 3)

	moving, _ := srv.Time()
	if moving <= age {
		t.Error("the unfrozen world's age did not move again")
	}
	// The released sand goes out of the world as a falling body.
	servertest.UntilTicks(t, srv, 100, "block at (3, 70, 3)", func() bool { return srv.BlockAt(3, 70, 3) == 0 })
	c.Disconnect()
}

// TestTickStepRunsAFrozenGameForward: the steps a frozen game is asked to
// run are broadcast in the Step Tick packet and run exactly that many game
// ticks; a time argument's count is what the packet carries, a stop cancels
// what is left, and an unfrozen game refuses a step.
func TestTickStepRunsAFrozenGameForward(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	srv, c := servertest.Join(t, cfg, "Testificate", servertest.OpsListing(t, "Testificate"))

	c.ChatCommand("tick freeze")
	c.ExpectTickingState(20, true)
	c.ExpectCommandFeedback("commands.tick.status.frozen")
	servertest.AwaitTicks(t, srv, 5)
	age, _ := srv.Time()

	// A bare step runs one tick.
	c.ChatCommand("tick step")
	c.ExpectTickingStep(1)
	one := int64(1)
	c.ExpectCommandFeedbackText("commands.tick.step.success", protocol.Text{Number: &one})
	servertest.UntilTicks(t, srv, 600, "condition", func() bool {
		at, _ := srv.Time()
		return at > age
	})
	servertest.AwaitTicks(t, srv, 5)
	age, _ = srv.Time()

	c.ChatCommand("tick step 5")
	c.ExpectTickingStep(5)
	five := int64(5)
	c.ExpectCommandFeedbackText("commands.tick.step.success", protocol.Text{Number: &five})
	servertest.UntilTicks(t, srv, 600, "condition", func() bool {
		at, _ := srv.Time()
		return at >= age+5
	})
	servertest.AwaitTicks(t, srv, 20)
	if at, _ := srv.Time(); at != age+5 {
		t.Errorf("the stepped game ran %d ticks, want 5", at-age)
	}

	// The steps ran out, so there is no step in progress to stop.
	c.ChatCommand("tick step stop")
	c.ExpectFailure("commands.tick.step.stop.fail")

	// A stopped step cancels the ticks left; some of the twenty may already
	// be in flight before the stop lands.
	c.ChatCommand("tick step 1s")
	c.ExpectTickingStep(20)
	twenty := int64(20)
	c.ExpectCommandFeedbackText("commands.tick.step.success", protocol.Text{Number: &twenty})
	c.ChatCommand("tick step stop")
	c.ExpectTickingStep(0)
	c.ExpectCommandFeedback("commands.tick.step.stop.success")
	servertest.AwaitTicks(t, srv, 30)
	if at, _ := srv.Time(); at > age+5+21 {
		t.Errorf("the stopped step still ran %d ticks of its twenty", at-(age+5))
	}

	// An unfrozen game refuses a step.
	c.ChatCommand("tick unfreeze")
	c.ExpectTickingState(20, false)
	c.ExpectCommandFeedback("commands.tick.status.running")
	c.ChatCommand("tick step 3")
	c.ExpectFailure("commands.tick.step.fail")

	c.ChatCommand("tick sprint stop")
	c.ExpectFailure("commands.tick.sprint.stop.fail")
	c.Disconnect()
}

// TestAFrozenWorldHoldsTheFurnaces: a freeze holds a loaded furnace's
// cooking, its fuel and its progress standing still, and unfreezing sets it
// going again.
// vanilla: Level.tickBlockEntities
func TestAFrozenWorldHoldsTheFurnaces(t *testing.T) {
	t.Parallel()
	srv, w := servertest.OpenWindowed(t, servertest.BlockTestConfig())
	servertest.StopTheLoop(t, srv)
	servertest.LoadFurnace(t, srv, w, "minecraft:raw_iron", 1, 1)

	srv.RunLine("tick freeze")
	// One whole frozen tick settles the state; the twenty after it are the
	// ones nothing may move on.
	srv.RunTick()
	spent, total, lit, _, ok := srv.FurnaceAt(4, 64, 0)
	if !ok {
		t.Fatal("the furnace's block entity went missing")
	}
	for range 20 {
		srv.RunTick()
	}
	spent2, total2, lit2, _, _ := srv.FurnaceAt(4, 64, 0)
	if spent2 != spent || total2 != total || lit2 != lit {
		t.Errorf("the frozen furnace cooked: (%d,%d,%d) -> (%d,%d,%d)", spent, total, lit, spent2, total2, lit2)
	}
	if spent != 0 || lit != 0 {
		t.Errorf("the furnace had cooked before the freeze lifted: spent %d, lit %d", spent, lit)
	}

	srv.RunLine("tick unfreeze")
	srv.RunTick()
	if _, _, lit3, _, _ := srv.FurnaceAt(4, 64, 0); lit3 == 0 {
		t.Error("the furnace did not light again after the freeze lifted")
	}
}

// TestAFrozenWorldStillShowsMobsWalkedIntoRange: the mobs' own tick is held
// by a freeze, but the tracking is not.
// vanilla: ChunkMap.tick
func TestAFrozenWorldStillShowsMobsWalkedIntoRange(t *testing.T) {
	t.Parallel()
	cfg := servertest.MobTestConfig()
	cfg.ViewDistance = 4
	srv, c := servertest.Join(t, cfg, "Walker")

	// Hold the mob's chunk so the herd keeps it while nobody stands there,
	// and plant the cow a hundred blocks east, past the range.
	release := srv.HoldChunk(100, 0)
	defer release()
	id := srv.SpawnMob("minecraft:cow", 100.5, 64, 0.5)
	if id == 0 {
		t.Fatal("SpawnMob came to nothing")
	}

	srv.RunLine("tick freeze")
	servertest.AwaitTicks(t, srv, 2)

	c.MoveToGround(50.5, 64, 0.5, true)
	got := c.AwaitSpawn(protocol.EntityTypeCow)
	if got.EntityID != id {
		t.Errorf("the frozen world showed cow %d, want %d", got.EntityID, id)
	}
	c.Disconnect()
}

// TestTickRateSetsTheLoopPace: /tick rate asks the loop to wait out the
// spacing of the new rate between its ticks — two a second here, after the
// twenty a second it starts at — and the console reads the rate back in
// the success line.
func TestTickRateSetsTheLoopPace(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	clock := newGateClock()
	srv, _ := servertest.Start(t, cfg, func(s *server.Server) { s.SetTickClock(clock, nil) })

	out := srv.RunLine("tick rate 2")
	if len(out) != 1 || out[0].Translate != "commands.tick.rate.success" ||
		len(out[0].With) != 1 || out[0].With[0].Text != "2.0" {
		t.Errorf("tick rate answer = %+v, want the one 2.0 rate.success line", out)
	}

	// The loop's first wait is on the spacing it started at; every wait
	// after waits the 500ms the rate of two paces by. One wait more is
	// released than paced, so the reads race no partial wait.
	clock.step()
	for range 6 {
		clock.step()
	}
	assertPacing(t, clock,
		50*time.Millisecond,
		500*time.Millisecond, 500*time.Millisecond, 500*time.Millisecond,
		500*time.Millisecond, 500*time.Millisecond, 500*time.Millisecond)
	if ticks := srv.TicksRun(); ticks < 6 {
		t.Errorf("the loop ran %d ticks, want at least the six released", ticks)
	}
}

// TestTickSprintRunsItsTicksBackToBack: a sprint's claimed ticks pace the
// loop at zero — back to back with no wait between them — and the sprint
// ends on its own with the report line, the pacing back at the rate's
// spacing after it.
func TestTickSprintRunsItsTicksBackToBack(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	clock := newGateClock()
	var logged strings.Builder
	log := slog.New(slog.NewTextHandler(&logged, nil))
	srv, _ := servertest.Start(t, cfg, func(s *server.Server) { s.SetTickClock(clock, log) })

	out := srv.RunLine("tick sprint 20")
	if len(out) != 1 || out[0].Translate != "commands.tick.status.sprinting" {
		t.Errorf("tick sprint answer = %+v, want the sprinting status line", out)
	}

	// The initial wait is the start's; the twenty sprint ticks then run
	// with no wait between them; the iteration that finishes the sprint
	// paces the plain rate spacing again, and so does the one after it.
	clock.step()
	for range 22 {
		clock.step()
	}
	wants := []time.Duration{50 * time.Millisecond}
	for range 20 {
		wants = append(wants, 0)
	}
	wants = append(wants, 50*time.Millisecond, 50*time.Millisecond)
	assertPacing(t, clock, wants...)
	if ticks := srv.TicksRun(); ticks < 21 {
		t.Errorf("the loop ran %d ticks, want at least the twenty-one a finished sprint left", ticks)
	}
	if !strings.Contains(logged.String(), "Sprint completed with") {
		t.Errorf("the finished sprint did not report:\n%s", logged.String())
	}
}

// TestTickQueryNamesTheFrozenAndSprintingState: the query's status line
// answers the state the game is in, the frozen one while it is frozen and
// the sprinting one while a sprint turns.
func TestTickQueryNamesTheFrozenAndSprintingState(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	srv, c := servertest.Join(t, cfg, "Testificate", servertest.OpsListing(t, "Testificate"))

	c.ChatCommand("tick freeze")
	c.ExpectTickingState(20, true)
	c.ExpectCommandFeedback("commands.tick.status.frozen")
	servertest.AwaitTicks(t, srv, 5)

	c.ChatCommand("tick query")
	c.ExpectCommandFeedback("commands.tick.status.frozen")
	c.ExpectFeedbackLine("commands.tick.query.rate.running")
	c.ExpectFeedbackLine("commands.tick.query.percentiles")

	c.ChatCommand("tick sprint 100000")
	c.ExpectTickingState(20, false)
	c.ExpectCommandFeedback("commands.tick.status.sprinting")
	servertest.UntilTicks(t, srv, 600, "condition", func() bool { return srv.TicksRun() >= 20 })

	c.ChatCommand("tick query")
	c.ExpectCommandFeedback("commands.tick.status.sprinting")
	c.ExpectFeedbackLine("commands.tick.query.rate.sprinting")
	c.ExpectFeedbackLine("commands.tick.query.percentiles")

	// The sprint was asked over a frozen world, so its end restores the
	// freeze.
	c.ChatCommand("tick sprint stop")
	c.AwaitSprintReport()
	c.ExpectTickingState(20, true)
	c.ExpectCommandFeedback("commands.tick.sprint.stop.success")
	c.Disconnect()
}
