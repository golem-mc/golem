package server

import (
	"fmt"
	"io"
	"log/slog"
	"math"
	"os"
	"path/filepath"
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
	b.Write(buf)
	return b.String()
}

// writeCrashReport writes report to path, creating its directory.
func writeCrashReport(path, report string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(report), 0o644)
}
