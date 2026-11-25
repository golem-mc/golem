package app

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime/debug"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const (
	crashDir      = "crash-reports"
	pendingPrefix = ".pending-"
	// crashTimeLayout is the time in the watchdog's report file name.
	// vanilla: CrashReport.saveToFile
	crashTimeLayout = "2006-01-02_15.04.05"
)

// crashOutput points the runtime's crash output at a pending file in
// dir/crash-reports, so a panic or fatal error no recover can catch (a
// concurrent map write, a goroutine without recover) leaves its trace on
// disk. It first gives a previous run's non-empty pending file its final name
// and warns. The returned function, called on a clean stop, removes the
// pending file when nothing was written to it.
func crashOutput(dir string) (pending string, cleanup func()) {
	cdir := filepath.Join(dir, crashDir)
	adoptPending(cdir)
	if err := os.MkdirAll(cdir, 0o755); err != nil {
		slog.Warn("could not create the crash report directory", "error", err)
		return "", func() {}
	}
	path := filepath.Join(cdir, fmt.Sprintf("%s%d.txt", pendingPrefix, os.Getpid()))
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		slog.Warn("could not open the crash report file", "error", err)
		return "", func() {}
	}
	// SetCrashOutput duplicates the descriptor, so f can be closed at once.
	err = debug.SetCrashOutput(f, debug.CrashOptions{})
	_ = f.Close()
	if err != nil {
		_ = os.Remove(path)
		slog.Warn("could not redirect the crash output", "error", err)
		return "", func() {}
	}
	return path, func() {
		_ = debug.SetCrashOutput(nil, debug.CrashOptions{})
		if st, err := os.Stat(path); err == nil && st.Size() == 0 {
			_ = os.Remove(path)
		}
	}
}

// adoptPending renames the pending files a crashed run left in cdir to
// crash-<mtime>-server.txt and logs a WARN for each; empty ones are removed.
// A file whose pid (in its name) is still running belongs to a live server
// and is left alone.
func adoptPending(cdir string) {
	files, _ := filepath.Glob(filepath.Join(cdir, pendingPrefix+"*.txt"))
	for _, p := range files {
		if pendingOwnerAlive(p) {
			continue
		}
		st, err := os.Stat(p)
		if err != nil {
			continue
		}
		if st.Size() == 0 {
			_ = os.Remove(p)
			continue
		}
		base := "crash-" + st.ModTime().Format(crashTimeLayout) + "-server"
		name := base + ".txt"
		for i := 1; ; i++ {
			if _, err := os.Stat(filepath.Join(cdir, name)); err != nil {
				break
			}
			name = fmt.Sprintf("%s-%d.txt", base, i)
		}
		if err := os.Rename(p, filepath.Join(cdir, name)); err != nil {
			slog.Warn("could not name the crash report of the previous run", "error", err)
			continue
		}
		slog.Warn("the previous run crashed; see " + crashDir + "/" + name)
	}
}

// pendingOwnerAlive reports whether the process whose pid is in the pending
// file's name still runs.
func pendingOwnerAlive(path string) bool {
	name := strings.TrimSuffix(strings.TrimPrefix(filepath.Base(path), pendingPrefix), ".txt")
	pid, err := strconv.Atoi(name)
	if err != nil || pid <= 0 {
		return false
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	err = proc.Signal(syscall.Signal(0))
	return err == nil || errors.Is(err, syscall.EPERM)
}

// logPanic, deferred in the main goroutine, logs a panic as an ERROR and
// panics again so the runtime writes the trace to the crash output, which
// the next start names.
func logPanic(pending string) {
	p := recover()
	if p == nil {
		return
	}
	slog.Error("The server crashed", "panic", p, "stack", string(debug.Stack()), "crash report", pending)
	panic(p)
}

// testCrashHook panics on a background goroutine when GOLEM_TEST_CRASH is
// "goroutine", for the test of the crash output.
func testCrashHook() {
	if os.Getenv("GOLEM_TEST_CRASH") == "goroutine" {
		go func() { panic("test crash") }()
		time.Sleep(time.Minute)
	}
}
