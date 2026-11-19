// Package tickrate is the main loop's ticking state (vanilla's
// TickRateManager): target rate, freeze, frozen steps and sprint. Packets and
// messages a change owes are package server's.
package tickrate

import (
	"math"
	"strconv"
	"strings"
	"sync"
	"time"
)

// MinRate is the least rate a set holds; a slower ask is clamped to it.
// vanilla: TickRateManager.MIN_TICKRATE
const MinRate = 1.0

// DefaultRate is the pace a loop starts at: twenty ticks a second.
const DefaultRate = 20.0

// DefaultNanos is the spacing DefaultRate paces by.
const DefaultNanos = int64(time.Second) / int64(DefaultRate)

// Manager is one loop's ticking state; every method is goroutine-safe.
type Manager struct {
	mu           sync.Mutex
	rate         float32
	nanosPerTick int64
	frozen       bool
	// steps is the ticks a frozen game still runs; running is settled once
	// per tick by Tick.
	steps   int
	running bool

	// Sprint: scheduled is the whole sprint, left the unclaimed ticks,
	// spent the claimed ones' work time, claimedAt the last claim's stamp,
	// frozenBefore the freeze to restore at the end.
	scheduled    int64
	left         int64
	spent        int64
	claimedAt    int64
	frozenBefore bool
}

// New returns a manager pacing a loop at DefaultRate.
func New() *Manager {
	return &Manager{
		rate:         DefaultRate,
		nanosPerTick: DefaultNanos,
		running:      true,
	}
}

// Tick settles whether the coming tick runs: unless frozen, or frozen with
// steps left, one of which it spends.
// vanilla: TickRateManager.tick
func (m *Manager) Tick() {
	m.mu.Lock()
	m.running = !m.frozen || m.steps > 0
	if m.steps > 0 {
		m.steps--
	}
	m.mu.Unlock()
}

// Running reports whether the game runs this tick.
func (m *Manager) Running() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.running
}

// Rate is the rate the loop aims at, in ticks a second.
func (m *Manager) Rate() float32 {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.rate
}

// NanosPerTick is the spacing the loop waits out between its ticks.
func (m *Manager) NanosPerTick() int64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.nanosPerTick
}

// SetRate aims the loop at rate, clamped to MinRate. NaN and infinities are
// refused and the spacing stays at least a nanosecond, so pacing never hits
// a zero interval.
// vanilla: TickRateManager.setTickRate
func (m *Manager) SetRate(rate float32) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if math.IsNaN(float64(rate)) || math.IsInf(float64(rate), 0) {
		return
	}
	m.rate = max(rate, MinRate)
	m.nanosPerTick = max(int64(float64(time.Second)/float64(m.rate)), 1)
}

// Frozen reports whether the game is held in a freeze.
func (m *Manager) Frozen() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.frozen
}

// SetFrozen freezes or unfreezes the game, leaving steps alone: an unfrozen
// game still runs steps it has left.
// vanilla: TickRateManager.setFrozen
func (m *Manager) SetFrozen(frozen bool) {
	m.mu.Lock()
	m.frozen = frozen
	m.mu.Unlock()
}

// Steps is the ticks a frozen game still has to run (Step Tick packet).
func (m *Manager) Steps() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.steps
}

// Step replaces a frozen game's steps left with ticks; false when not frozen.
// vanilla: ServerTickRateManager.stepGameIfPaused
func (m *Manager) Step(ticks int) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.frozen {
		return false
	}
	m.steps = ticks
	return true
}

// StopStep cancels a frozen game's steps left, reporting whether any were.
// vanilla: ServerTickRateManager.stopStepping
func (m *Manager) StopStep() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.steps == 0 {
		return false
	}
	m.steps = 0
	return true
}

// Sprinting reports a sprint whose ticks the loop has not all claimed.
func (m *Manager) Sprinting() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.scheduled > 0
}

// Sprint runs ticks unpaced, unfrozen until it ends and the freeze is
// restored. interrupted reports a running sprint that was thrown away.
// vanilla: ServerTickRateManager.requestGameToSprint
func (m *Manager) Sprint(ticks int) (interrupted bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	interrupted = m.left > 0
	m.scheduled = int64(ticks)
	m.left = int64(ticks)
	m.spent = 0
	m.frozenBefore = m.frozen
	m.frozen = false
	return interrupted
}

// ClaimSprintTick claims a sprint tick for this loop iteration, now being the
// stamp its work time starts at. sprint is false when the iteration paces
// normally, which is also how an exhausted sprint ends: finished then carries
// its report, the freeze already restored.
// vanilla: ServerTickRateManager.checkShouldSprintThisTick, finishTickSprint
func (m *Manager) ClaimSprintTick(now int64) (sprint bool, finished *SprintReport) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.running {
		return false, nil
	}
	if m.left > 0 {
		m.claimedAt = now
		m.left--
		return true, nil
	}
	if m.scheduled == 0 {
		return false, nil
	}
	finished = m.finish()
	return false, finished
}

// EndSprintTick adds the time from the tick's claim to now, any wait
// included, to the sprint's work time.
// vanilla: ServerTickRateManager.endTickWork
func (m *Manager) EndSprintTick(now int64) {
	m.mu.Lock()
	m.spent += now - m.claimedAt
	m.mu.Unlock()
}

// StopSprint ends a running sprint at once; stopped is false if none was.
// vanilla: ServerTickRateManager.stopSprinting, finishTickSprint
func (m *Manager) StopSprint() (report *SprintReport, stopped bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.left == 0 {
		return nil, false
	}
	return m.finish(), true
}

// finish clears the sprint, restores its freeze and builds the report. m.mu
// is held.
// vanilla: ServerTickRateManager.finishTickSprint
func (m *Manager) finish() *SprintReport {
	completed := m.scheduled - m.left
	msToComplete := math.Max(1, float64(m.spent)) / 1e6
	tps := int64(1000 * float64(completed) / msToComplete)
	msPerTick := msToComplete / float64(completed)
	if completed == 0 {
		// millisecondsPerTick is a float widened for the format.
		// vanilla: TickRateManager.millisecondsPerTick
		msPerTick = float64(float32(m.nanosPerTick) / 1e6)
	}
	report := &SprintReport{TicksPerSecond: tps, MSPerTick: javaTwoDecimals(msPerTick)}
	m.scheduled = 0
	m.spent = 0
	m.left = 0
	m.frozen = m.frozenBefore
	return report
}

// SprintReport is a finished sprint's whole ticks per second and average
// milliseconds per tick.
// vanilla: ServerTickRateManager.finishTickSprint
type SprintReport struct {
	TicksPerSecond int64
	MSPerTick      string
}

// javaTwoDecimals is Java's String.format("%.2f"): shortest decimal digits
// rounded half up, not Go's round-half-even on the exact binary value.
func javaTwoDecimals(v float64) string {
	s := strconv.FormatFloat(v, 'f', -1, 64)
	sign := ""
	if strings.HasPrefix(s, "-") {
		sign, s = "-", s[1:]
	}
	whole, frac, _ := strings.Cut(s, ".")
	frac += "000"
	digits := []byte(whole + frac[:2])
	if frac[2] >= '5' {
		i := len(digits) - 1
		for ; i >= 0 && digits[i] == '9'; i-- {
			digits[i] = '0'
		}
		if i < 0 {
			digits = append([]byte{'1'}, digits...)
		} else {
			digits[i]++
		}
	}
	return sign + string(digits[:len(digits)-2]) + "." + string(digits[len(digits)-2:])
}
