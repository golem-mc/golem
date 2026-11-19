package console

import (
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/golem-mc/golem/internal/protocol"
)

// errRollFails is what the failing gzip the tests put in reports.
var errRollFails = errors.New("the roll failed")

// readLatest reads dir's latest.log back.
func readLatest(t *testing.T, dir string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, latestLog))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// gunzip reads a rolled log back.
func gunzip(t *testing.T, path string) string {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	z, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	b, err := io.ReadAll(z)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// leaveLatest leaves a latest.log behind as a run before would have, last
// written on the day given.
func leaveLatest(t *testing.T, dir, content string, day time.Time) {
	t.Helper()
	path := filepath.Join(dir, latestLog)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, day, day); err != nil {
		t.Fatal(err)
	}
}

// TestLogFileTakesTheConsoleLines has the handler draw a line in colour on
// the console and holds that the log file takes the same lines without the
// colour codes.
func TestLogFileTakesTheConsoleLines(t *testing.T) {
	dir := t.TempDir()
	logs, err := OpenLogFile(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer logs.Close()

	var out bytes.Buffer
	h := NewLogHandler(&out, slog.LevelInfo, true).WithFile(logs)
	line := protocol.Text{Text: "[Not Secure] ", Extra: []protocol.Text{{Text: "hi", Color: protocol.ColorRed}}}
	handle(t, h, slog.LevelInfo, line.Plain(), protocol.LogComponentKey, line)
	handle(t, h, slog.LevelWarn, "careful")

	want := "[21:04:05] [Server thread/INFO]: [Not Secure] hi\n[21:04:05] [Server thread/WARN]: careful\n"
	if got := readLatest(t, dir); got != want {
		t.Errorf("latest.log is %q, want %q", got, want)
	}
	if !strings.Contains(out.String(), "\x1b[0;91mhi\x1b[0m") {
		t.Errorf("the console lost its colours: %q", out.String())
	}
}

// TestLogFileRotatesTheRunBefore starts the log on a latest.log left by the
// run before: the old lines are gzipped away under the day the file was
// last written on and the fresh latest.log takes the new lines.
func TestLogFileRotatesTheRunBefore(t *testing.T) {
	dir := t.TempDir()
	leaveLatest(t, dir, "the run before\n", time.Date(2024, 5, 1, 12, 0, 0, 0, time.Local))

	logs, err := OpenLogFile(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer logs.Close()

	if got := gunzip(t, filepath.Join(dir, "2024-05-01-1.log.gz")); got != "the run before\n" {
		t.Errorf("the rolled log is %q, want the run before", got)
	}
	if got := readLatest(t, dir); got != "" {
		t.Errorf("the fresh latest.log is %q, want it empty", got)
	}
	if _, err := logs.Write([]byte("the new run\n")); err != nil {
		t.Fatal(err)
	}
	if got := readLatest(t, dir); got != "the new run\n" {
		t.Errorf("latest.log is %q, want the new run", got)
	}
}

// TestLogFileRollsWhenTheDayTurns has a line written after the day turned
// move the file the day before's lines went into, named by the day they
// were written on.
func TestLogFileRollsWhenTheDayTurns(t *testing.T) {
	dir := t.TempDir()
	logs, err := OpenLogFile(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer logs.Close()

	if _, err := logs.Write([]byte("the day before\n")); err != nil {
		t.Fatal(err)
	}
	// The rolled file is named by its last-modified day, read off before
	// the clock is put past it.
	info, err := os.Stat(filepath.Join(dir, latestLog))
	if err != nil {
		t.Fatal(err)
	}
	day := info.ModTime()
	logs.now = func() time.Time { return day.AddDate(0, 0, 1) }

	if _, err := logs.Write([]byte("the day after\n")); err != nil {
		t.Fatal(err)
	}
	if got := gunzip(t, filepath.Join(dir, day.Format(dayFormat)+"-1.log.gz")); got != "the day before\n" {
		t.Errorf("the rolled log is %q, want the day before", got)
	}
	if got := readLatest(t, dir); got != "the day after\n" {
		t.Errorf("latest.log is %q, want the day after", got)
	}
}

// TestLogFileSurvivesAFailedRoll has a roll that fails take the line into
// the open file all the same, reported once, with the roll not retried on
// every line after.
func TestLogFileSurvivesAFailedRoll(t *testing.T) {
	dir := t.TempDir()
	logs, err := OpenLogFile(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer logs.Close()

	if _, err := logs.Write([]byte("the day before\n")); err != nil {
		t.Fatal(err)
	}
	// The rolled file would be named by latest.log's last-modified day, read
	// off before the clock is put past it.
	info, err := os.Stat(filepath.Join(dir, latestLog))
	if err != nil {
		t.Fatal(err)
	}
	day := info.ModTime()
	logs.now = func() time.Time { return day.AddDate(0, 0, 1) }
	logs.gzip = func(string) error { return errRollFails }

	n, err := logs.Write([]byte("the day after\n"))
	if !errors.Is(err, errRollFails) {
		t.Errorf("the failed roll reported %v, want %v", err, errRollFails)
	}
	if n != len("the day after\n") {
		t.Errorf("the failed roll wrote %d bytes, want the line taken all the same", n)
	}
	if got := readLatest(t, dir); got != "the day before\nthe day after\n" {
		t.Errorf("latest.log is %q, want the line in the open file", got)
	}

	if _, err := logs.Write([]byte("more\n")); err != nil {
		t.Errorf("the failed roll was retried on the next line: %v", err)
	}
	if got := readLatest(t, dir); got != "the day before\nthe day after\nmore\n" {
		t.Errorf("latest.log is %q, want every line kept", got)
	}
}

// TestLogFileTakesConcurrentWrites has writes from several goroutines land
// whole and all of them there; under -race it is the log's lock at work.
func TestLogFileTakesConcurrentWrites(t *testing.T) {
	dir := t.TempDir()
	logs, err := OpenLogFile(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer logs.Close()

	const writers, lines = 4, 25
	allowed := make(map[string]bool, writers)
	for w := range writers {
		allowed[fmt.Sprintf("writer %d", w)] = true
	}
	var wg sync.WaitGroup
	for w := range writers {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			line := []byte(fmt.Sprintf("writer %d\n", w))
			for range lines {
				if _, err := logs.Write(line); err != nil {
					t.Error(err)
					return
				}
			}
		}(w)
	}
	wg.Wait()

	got := readLatest(t, dir)
	if count := strings.Count(got, "\n"); count != writers*lines {
		t.Errorf("latest.log took %d lines, want %d", count, writers*lines)
	}
	for line := range strings.SplitSeq(strings.TrimSuffix(got, "\n"), "\n") {
		if !allowed[line] {
			t.Errorf("a line came out mangled: %q", line)
		}
	}
}

// TestLogFileCloseWaitsForARollInFlight has a Close that lands while a roll
// is archiving the old file wait for it: the compression and the fresh open
// finish before Close returns, so nothing of the roll writes behind the stop.
func TestLogFileCloseWaitsForARollInFlight(t *testing.T) {
	dir := t.TempDir()
	logs, err := OpenLogFile(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := logs.Write([]byte("the day before\n")); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(dir, latestLog))
	if err != nil {
		t.Fatal(err)
	}
	logs.now = func() time.Time { return info.ModTime().AddDate(0, 0, 1) }

	// The roll is held open in gzip until the test has called Close, so
	// Close has to be waiting on it.
	started := make(chan struct{})
	release := make(chan struct{})
	logs.gzip = func(d string) error {
		close(started)
		<-release
		return gzipLatest(d)
	}
	written := make(chan error, 1)
	go func() {
		_, err := logs.Write([]byte("the day after\n"))
		written <- err
	}()
	<-started
	closed := make(chan error, 1)
	go func() { closed <- logs.Close() }()
	select {
	case err := <-closed:
		t.Fatalf("Close returned while the roll was still compressing: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	close(release)

	if err := <-written; err != nil {
		t.Fatal(err)
	}
	if err := <-closed; err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, info.ModTime().Format(dayFormat)+"-1.log.gz")); err != nil {
		t.Errorf("the roll's archive is missing after Close: %v", err)
	}
	if got := readLatest(t, dir); got != "the day after\n" {
		t.Errorf("latest.log is %q, want the line the roll took", got)
	}
}
