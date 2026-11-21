package server

import (
	"fmt"
	"io"
	"log/slog"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"
)

// watchdogExitDelay is how long the shutdown after a watchdog crash may take
// before the process exits anyway.
// vanilla: ServerWatchdog.exit (Runtime.halt after 10000 ms)
const watchdogExitDelay = 10 * time.Second

// StartWatchdog watches the main loop for a tick that overruns max-tick-time
// and then ends the process through exit, after writing a crash report under
// dir/crash-reports and printing it to out. It starts nothing while max-tick-time is 0 or less.
// vanilla: DedicatedServer.initServer, ServerWatchdog
func (s *Server) StartWatchdog(dir string, out io.Writer, exit func(code int)) {
	ms := s.Config.MaxTickTime
	if ms <= 0 {
		return
	}
	// Clamped so a huge key cannot overflow the Duration.
	maxTick := time.Duration(min(ms, math.MaxInt64/int64(time.Millisecond))) * time.Millisecond
	go s.watchdog(dir, out, maxTick, exit)
}

// watchdog runs until a stop is requested, which ends the watch the way
// vanilla's isRunning does, or until it finds a tick overrunning maxTick.
// vanilla: ServerWatchdog.run
func (s *Server) watchdog(dir string, out io.Writer, maxTick time.Duration, exit func(code int)) {
	stop := s.StopRequested()
	for {
		clock := s.loopClock()
		now := clock.Now()
		// Before the loop's first tick nothing is published; look again
		// a max-tick-time later.
		deadline := now.Add(maxTick)
		if next := s.nextTickNanos.Load(); next != 0 {
			start := time.Unix(0, next)
			if overrun := now.Sub(start); overrun > maxTick {
				s.watchdogCrash(dir, out, overrun, exit)
				return
			}
			// One nanosecond past: the check is strict, so a wait to the
			// limit itself would find nothing and return at once, again.
			deadline = start.Add(maxTick + 1)
		}
		if !clock.Wait(deadline, stop) {
			return
		}
	}
}

// watchdogCrash reports the hung tick, asks for the shutdown that saves the
// world, and calls exit(1) once the loop clock reads watchdogExitDelay later,
// whether or not that shutdown finished.
func (s *Server) watchdogCrash(dir string, out io.Writer, overrun time.Duration, exit func(code int)) {
	slog.Error(fmt.Sprintf("A single server tick took %.2f seconds (should be max %.2f)",
		overrun.Seconds(), float64(s.tickRate.NanosPerTick())/float64(time.Second)))
	slog.Error("Considering it to be crashed, server will forcibly shutdown.")

	now := time.Now()
	report := watchdogReport(now, s.Config.ServerID)
	fmt.Fprintln(out, "Crash report:\n"+report)
	path := filepath.Join(dir, "crash-reports", "crash-"+now.Format("2006-01-02_15.04.05")+"-server.txt")
	if err := writeCrashReport(path, report); err != nil {
		slog.Error("We were unable to save this crash report to disk.", "error", err)
	} else {
		if abs, err := filepath.Abs(path); err == nil {
			path = abs
		}
		slog.Error("This crash report has been saved to: " + path)
	}

	// The exit's deadline is read before the stop request, so whoever sees
	// the stop also sees the deadline counted from before it.
	clock := s.loopClock()
	deadline := clock.Now().Add(watchdogExitDelay)
	s.crashed.Store(true)
	s.RequestStop()
	clock.Wait(deadline, nil)
	exit(1)
}

// watchdogReport is the crash report's text: vanilla's header lines, then
// every goroutine's stack in place of vanilla's thread dump.
// vanilla: CrashReport.getFriendlyReport, ServerWatchdog.createWatchdogCrashReport
func watchdogReport(now time.Time, serverID string) string {
	buf := make([]byte, 64<<10)
	for {
		n := runtime.Stack(buf, true)
		if n < len(buf) {
			buf = buf[:n]
			break
		}
		buf = make([]byte, 2*len(buf))
	}
	var b strings.Builder
	b.WriteString("---- Minecraft Crash Report ----\n")
	b.WriteString("Time: " + now.Format("2006-01-02 15:04:05") + "\n")
	b.WriteString("Description: Watching Server\n")
	// vanilla: MinecraftServer.fillSystemReport
	if serverID != "" {
		b.WriteString("Server Id: " + serverID + "\n")
	}
	b.WriteString("\n")
	b.WriteString(groupStacks(string(buf)))
	return b.String()
}

var (
	goroutineHeader = regexp.MustCompile(`^goroutine \d+ \[([^\],]*)(?:, [^\]]*)?\](?: (\{.*\}))?:$`)
	frameArgs       = regexp.MustCompile(`\(.*\)$`)
	frameOffset     = regexp.MustCompile(` \+0x[0-9a-f]+$`)
	createdByID     = regexp.MustCompile(` in goroutine \d+$`)
)

// groupStacks rewrites a runtime.Stack(all) dump: the goroutine running
// tickLoop first, then one entry per distinct state and frames, headed by
// its count when more than one goroutine shares it. Goroutines with different
// pprof label sets stay apart. A grouped entry shows the
// first goroutine's stack and no goroutine ID.
func groupStacks(dump string) string {
	type group struct {
		text  string
		count int
		tick  bool
	}
	var groups []*group
	index := map[string]*group{}
	for block := range strings.SplitSeq(strings.TrimSpace(dump), "\n\n") {
		lines := strings.Split(block, "\n")
		m := goroutineHeader.FindStringSubmatch(lines[0])
		if m == nil {
			// Not a goroutine block: keep it as it is.
			groups = append(groups, &group{text: block, count: 1})
			continue
		}
		// The key leaves out what differs between goroutines doing the same
		// thing: IDs, argument values, pc offsets and wait times.
		key := []string{m[1], m[2]}
		for _, l := range lines[1:] {
			if !strings.HasPrefix(l, "\t") {
				l = frameArgs.ReplaceAllString(l, "")
			}
			l = frameOffset.ReplaceAllString(l, "")
			key = append(key, createdByID.ReplaceAllString(l, ""))
		}
		k := strings.Join(key, "\n")
		if g := index[k]; g != nil {
			g.count++
			continue
		}
		g := &group{text: block, count: 1, tick: strings.Contains(block, ".tickLoop(")}
		index[k] = g
		groups = append(groups, g)
	}
	var b strings.Builder
	write := func(g *group) {
		text := g.text
		if g.count > 1 {
			lines := strings.SplitN(text, "\n", 2)
			hm := goroutineHeader.FindStringSubmatch(lines[0])
			state, labels := hm[1], hm[2]
			if labels != "" {
				labels = " " + labels
			}
			rest := ""
			if len(lines) > 1 {
				rest = "\n" + lines[1]
			}
			text = fmt.Sprintf("%d goroutines [%s]%s:%s", g.count, state, labels, rest)
		}
		b.WriteString(text + "\n\n")
	}
	for _, g := range groups {
		if g.tick {
			write(g)
		}
	}
	for _, g := range groups {
		if !g.tick {
			write(g)
		}
	}
	return b.String()
}

// writeCrashReport writes report to path, creating its directory.
func writeCrashReport(path, report string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(report), 0o644)
}
