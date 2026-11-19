package tickrate

import (
	"testing"
)

// TestTickGatesOnFrozenStateAndSteps: the game runs until a freeze takes
// it, runs the steps the freeze is given and no more, and runs again once
// unfrozen — the steps an unfrozen game is given still running.
// vanilla: TickRateManager.tick
func TestTickGatesOnFrozenStateAndSteps(t *testing.T) {
	m := New()
	if !m.Running() {
		t.Fatal("a fresh world does not run")
	}
	m.Tick()
	if !m.Running() {
		t.Error("the fresh world stopped running")
	}

	if m.Step(3) {
		t.Error("a step worked on a world that is not frozen")
	}
	m.SetFrozen(true)
	if !m.Step(3) {
		t.Fatal("a step was refused on a frozen world")
	}
	for want := 3; want >= 1; want-- {
		m.Tick()
		if !m.Running() {
			t.Fatalf("the frozen world stopped before its %d steps ran", want)
		}
	}
	m.Tick()
	if m.Running() {
		t.Error("the frozen world ran past its steps")
	}

	m.SetFrozen(false)
	m.Tick()
	if !m.Running() {
		t.Error("the unfrozen world did not run")
	}
	if m.Step(2) {
		t.Error("a step was given to a world that is not frozen")
	}
}

// TestStepStopCancelsStepsLeft: stop clears the steps left, reports whether
// there were any, and a later step replaces the ones still running.
func TestStepStopCancelsStepsLeft(t *testing.T) {
	m := New()
	if m.StopStep() {
		t.Error("stopping reported steps on a world that was given none")
	}
	m.SetFrozen(true)
	m.Step(5)
	if got := m.Steps(); got != 5 {
		t.Fatalf("steps = %d, want 5", got)
	}
	// A second step replaces the first's count.
	m.Step(4)
	if got := m.Steps(); got != 4 {
		t.Fatalf("steps after re-asking = %d, want 4", got)
	}
	if !m.StopStep() {
		t.Fatal("stopping reported no steps in progress")
	}
	if got := m.Steps(); got != 0 {
		t.Errorf("steps after stop = %d, want 0", got)
	}
	if m.StopStep() {
		t.Error("stopping reported steps twice")
	}
}

// TestSetRateClampsAndPaces: a rate below the least is clamped to it, and
// the spacing the loop waits is a second over the rate, whole nanoseconds.
// vanilla: TickRateManager.setTickRate
func TestSetRateClampsAndPaces(t *testing.T) {
	m := New()
	if got, want := m.Rate(), float32(20); got != want {
		t.Fatalf("rate = %v, want %v", got, want)
	}
	if got, want := m.NanosPerTick(), int64(50_000_000); got != want {
		t.Fatalf("nanos = %d, want %d", got, want)
	}
	m.SetRate(0.5)
	if got := m.Rate(); got != MinRate {
		t.Errorf("rate under the least = %v, want %v", got, MinRate)
	}
	m.SetRate(10)
	if got, want := m.NanosPerTick(), int64(100_000_000); got != want {
		t.Errorf("nanos at ten a second = %d, want %d", got, want)
	}
}

// TestSprintClaimsTicksAndFinishes: a sprint unfreezes the game for its
// claimed ticks, ends on its own with the completed counts when none are
// left and restores the freeze it found.
// vanilla: ServerTickRateManager.requestGameToSprint, checkShouldSprintThisTick
func TestSprintClaimsTicksAndFinishes(t *testing.T) {
	m := New()
	m.SetFrozen(true)

	if m.Sprint(3) {
		t.Error("a sprint reported interrupting on a world with no sprint")
	}
	if m.Frozen() {
		t.Error("the sprint left the world frozen")
	}
	m.Tick() // settle running=true: a sprint starts on a frozen world's state
	for range 3 {
		sprint, finished := m.ClaimSprintTick(0)
		if !sprint || finished != nil {
			t.Fatalf("claim of an in-flight sprint = %v, %v", sprint, finished)
		}
		m.EndSprintTick(1_000_000) // a millisecond a tick
	}
	sprint, finished := m.ClaimSprintTick(0)
	if sprint || finished == nil {
		t.Fatalf("the out-of-sprint claim = %v, %v, want the finish report", sprint, finished)
	}
	if m.Sprinting() || !m.Frozen() {
		t.Error("the finished sprint did not put the world back the freeze it found")
	}
	// Three ticks of a millisecond each: a thousand a second, 1.00ms a
	// tick.
	if got, want := finished.TicksPerSecond, int64(1000); got != want {
		t.Errorf("ticks per second = %d, want %d", got, want)
	}
	if got, want := finished.MSPerTick, "1.00"; got != want {
		t.Errorf("ms per tick = %q, want %q", got, want)
	}
}

// TestSprintStopReportsTheUnfinished: stopping a sprint reports only the
// ticks it completed, and a second stop reports none.
// vanilla: ServerTickRateManager.stopSprinting
func TestSprintStopReportsTheUnfinished(t *testing.T) {
	m := New()
	m.Sprint(10)
	m.Tick()
	for range 4 {
		sprint, _ := m.ClaimSprintTick(0)
		if !sprint {
			t.Fatal("a claim of a turning sprint was refused")
		}
		m.EndSprintTick(2_000_000) // two milliseconds a tick
	}
	report, stopped := m.StopSprint()
	if !stopped {
		t.Fatal("the stop reported no sprint in progress")
	}
	if got, want := report.TicksPerSecond, int64(500); got != want {
		t.Errorf("ticks per second = %d, want %d", got, want)
	}
	if got, want := report.MSPerTick, "2.00"; got != want {
		t.Errorf("ms per tick = %q, want %q", got, want)
	}
	if _, stopped := m.StopSprint(); stopped {
		t.Error("a second stop reported a sprint in progress")
	}
	if m.Sprinting() {
		t.Error("the stopped sprint is still sprinting")
	}
}

// TestJavaTwoDecimals rounds half up on the shortest digits, where Go's
// formatting rounds ties to even.
func TestJavaTwoDecimals(t *testing.T) {
	for _, c := range []struct {
		in   float64
		want string
	}{
		{0, "0.00"},
		{0.005, "0.01"},
		{0.004, "0.00"},
		{0.125, "0.13"}, // Go rounds this tie to 0.12
		{1.999, "2.00"},
		{2.675, "2.68"},
		{9.995, "10.00"},
		{50, "50.00"},
		{50.12345, "50.12"},
		{1e-9, "0.00"},
	} {
		if got := javaTwoDecimals(c.in); got != c.want {
			t.Errorf("javaTwoDecimals(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}
