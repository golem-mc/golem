package tickrhythm

import (
	"bytes"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// stallClock is a simulated clock for the main loop: each wait jumps the
// time straight to the next tick, and the ticks named in stalls take that
// long to run. Past end the loop is parked until it is stopped.
type stallClock struct {
	mu     sync.Mutex
	now    time.Time
	end    time.Time
	stalls map[int]time.Duration // by tick, counted from 1
	waits  int
	parked chan struct{}
}

func newStallClock(run time.Duration, stalls map[int]time.Duration) *stallClock {
	start := time.Unix(1_000_000, 0)
	return &stallClock{now: start, end: start.Add(run), stalls: stalls, parked: make(chan struct{})}
}

func (c *stallClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

// Wait follows the tick just run, so that is where its stall is added. The
// first wait comes before any tick.
func (c *stallClock) Wait(t time.Time, done <-chan struct{}) bool {
	c.mu.Lock()
	c.now = c.now.Add(c.stalls[c.waits])
	c.waits++
	if t.After(c.end) {
		c.mu.Unlock()
		close(c.parked)
		<-done
		return false
	}
	if t.After(c.now) {
		c.now = t
	}
	c.mu.Unlock()
	select {
	case <-done:
		return false
	default:
		return true
	}
}

// runStalled runs the main loop for five simulated seconds with the given
// stalls and returns the ticks it ran and what it logged.
func runStalled(t *testing.T, stalls map[int]time.Duration) (uint64, string) {
	t.Helper()
	clock := newStallClock(5*time.Second, stalls)
	// The loop logs before it parks, so the buffer is read after it alone.
	var logged bytes.Buffer
	log := slog.New(slog.NewTextHandler(&logged, nil))
	srv, _ := servertest.Start(t, servertest.BlockTestConfig(), func(s *server.Server) { s.SetTickClock(clock, log) })
	select {
	case <-clock.parked:
	case <-time.After(30 * time.Second):
		t.Fatal("the main loop did not reach the end of the simulated run")
	}
	return srv.TicksRun(), logged.String()
}

const overloadWarning = "Can't keep up! Is the server overloaded?"

// TestTickLoopCatchesUpAStall checks that the twenty ticks a one-second tick
// held up are run late rather than skipped: five seconds hold all 100 ticks,
// at 50 ms through 5 s.
func TestTickLoopCatchesUpAStall(t *testing.T) {
	t.Parallel()
	ticks, logged := runStalled(t, map[int]time.Duration{10: time.Second})
	if ticks != 100 {
		t.Errorf("ran %d ticks in five seconds with a one-second stall, want all 100", ticks)
	}
	if strings.Contains(logged, overloadWarning) {
		t.Errorf("a one-second stall was warned of:\n%s", logged)
	}
}

// TestTickLoopDropsABacklogPastTwoSeconds checks that a loop more than two
// seconds behind gives up the backlog with one warning: the tenth tick
// running three seconds leaves it 2950 ms or 59 ticks behind, which are
// dropped.
func TestTickLoopDropsABacklogPastTwoSeconds(t *testing.T) {
	t.Parallel()
	ticks, logged := runStalled(t, map[int]time.Duration{10: 3 * time.Second})
	if ticks != 100-59 {
		t.Errorf("ran %d ticks in five seconds with a three-second stall, want %d", ticks, 100-59)
	}
	if n := strings.Count(logged, overloadWarning); n != 1 {
		t.Fatalf("warned %d times, want once:\n%s", n, logged)
	}
	if !strings.Contains(logged, "Running 2950ms or 59 ticks behind") {
		t.Errorf("the warning does not give the backlog:\n%s", logged)
	}
}
