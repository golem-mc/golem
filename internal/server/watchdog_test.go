package server

import (
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/golem-mc/golem/internal/config"
)

// fakeWatchClock is a tickClock the test moves by hand: Wait returns once
// the test has set the time to its argument or past it.
type fakeWatchClock struct {
	mu  sync.Mutex
	now time.Time
	// moved is closed and replaced on every set, waking the waiters.
	moved chan struct{}
	// waits gets each Wait's target while it has room, so the test can
	// move the clock to where the loop waits.
	waits chan time.Time
}

func (c *fakeWatchClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeWatchClock) set(t time.Time) {
	c.mu.Lock()
	c.now = t
	close(c.moved)
	c.moved = make(chan struct{})
	c.mu.Unlock()
}

func (c *fakeWatchClock) Wait(t time.Time, done <-chan struct{}) bool {
	select {
	case c.waits <- t:
	default:
	}
	for {
		c.mu.Lock()
		now, moved := c.now, c.moved
		c.mu.Unlock()
		if !now.Before(t) {
			return true
		}
		select {
		case <-moved:
		case <-done:
			return false
		}
	}
}

// startHungTick starts a server whose loop runs on a fake clock and whose
// first tick blocks until the test ends, and returns the clock and the
// tick's scheduled start as the loop published it.
func startHungTick(t *testing.T, maxTickTime int64) (*Server, *fakeWatchClock, time.Time) {
	t.Helper()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.LevelName = t.TempDir()
	cfg.LevelType = TestFlatLevelType
	cfg.MaxTickTime = maxTickTime
	s, err := newServer(cfg, 1)
	if err != nil {
		t.Fatal(err)
	}
	clock := &fakeWatchClock{now: time.Unix(1_000_000_000, 0), moved: make(chan struct{}), waits: make(chan time.Time, 1)}
	s.tickClockMu.Lock()
	s.tickClock = clock
	s.tickClockMu.Unlock()

	entered := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	fault := func() {
		once.Do(func() { close(entered) })
		<-release
	}
	s.tickFault.Store(&fault)
	s.startTick()
	t.Cleanup(func() {
		close(release)
		s.Shutdown()
	})

	// The loop's first wait is the one in waits: nothing else waits yet.
	clock.set(<-clock.waits)
	<-entered
	return s, clock, time.Unix(0, s.nextTickNanos.Load())
}

func stopRequested(s *Server) bool {
	select {
	case <-s.StopRequested():
		return true
	default:
		return false
	}
}

// TestWatchdogCrashesAServerWhoseTickOverrunsMaxTickTime checks that a tick
// running past max-tick-time requests the crash stop, writes the crash
// report and exits with 1 ten seconds later.
func TestWatchdogCrashesAServerWhoseTickOverrunsMaxTickTime(t *testing.T) {
	s, clock, start := startHungTick(t, 100)
	s.Config.ServerID = "lobby-1"
	dir := t.TempDir()
	exited := make(chan int, 1)
	s.StartWatchdog(dir, io.Discard, func(code int) { exited <- code })

	clock.set(start.Add(99 * time.Millisecond))
	if stopRequested(s) {
		t.Fatal("a stop was requested 99ms into a tick with max-tick-time 100")
	}

	clock.set(start.Add(101 * time.Millisecond))
	<-s.StopRequested()
	if !s.Crashed() {
		t.Fatal("Crashed() = false after the watchdog fired")
	}
	select {
	case code := <-exited:
		t.Fatalf("exit(%d) before the shutdown's 10s", code)
	default:
	}

	entries, err := os.ReadDir(filepath.Join(dir, "crash-reports"))
	if err != nil {
		t.Fatal(err)
	}
	name := regexp.MustCompile(`^crash-\d{4}-\d{2}-\d{2}_\d{2}\.\d{2}\.\d{2}-server\.txt$`)
	if len(entries) != 1 || !name.MatchString(entries[0].Name()) {
		t.Fatalf("crash-reports holds %v, want one crash-<time>-server.txt", entries)
	}
	report, err := os.ReadFile(filepath.Join(dir, "crash-reports", entries[0].Name()))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Description: Watching Server\nServer Id: lobby-1\n", "tickLoop"} {
		if !strings.Contains(string(report), want) {
			t.Errorf("the crash report lacks %q", want)
		}
	}

	clock.set(clock.Now().Add(watchdogExitDelay))
	if code := <-exited; code != 1 {
		t.Fatalf("exit(%d), want exit(1)", code)
	}
}

// TestWatchdogIsOffAtMaxTickTimeZero checks that max-tick-time 0 watches
// nothing: a tick hung for a minute requests no stop.
func TestWatchdogIsOffAtMaxTickTimeZero(t *testing.T) {
	s, clock, start := startHungTick(t, 0)
	s.StartWatchdog(t.TempDir(), io.Discard, func(code int) { t.Errorf("exit(%d) with the watchdog off", code) })
	clock.set(start.Add(time.Minute))
	if stopRequested(s) {
		t.Fatal("a stop was requested with max-tick-time 0")
	}
}
