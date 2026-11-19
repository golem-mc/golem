package console

import (
	"compress/gzip"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"
)

// latestLog is the file the running log is appended to.
const latestLog = "latest.log"

// dayFormat is the yyyy-MM-dd a rolled log is named by.
const dayFormat = "2006-01-02"

// errClosed is what Write reports once Close has run, rather than open
// latest.log again behind the run.
var errClosed = errors.New("the log file is closed")

// LogFile appends to <dir>/latest.log and gzips one left over to
// <dir>/<yyyy-MM-dd>-<n>.log.gz, the day being its last-modified day and n
// counting from 1. Unlike log4j there is no cap of 7 archives a day.
// vanilla: log4j2.xml in com.mojang:logging, OnStartupTriggeringPolicy and
// TimeBasedTriggeringPolicy with filePattern logs/%d{yyyy-MM-dd}-%i.log.gz
type LogFile struct {
	dir string
	// now and gzip are replaced by the tests.
	now  func() time.Time
	gzip func(string) error

	mu   sync.Mutex
	file *os.File
	// day is the day the open file is logged under. Write and openLogFile
	// set it even when a roll fails, so a failed roll is not retried on
	// every line; roll sets it only once the file has turned over.
	day time.Time
	// closed makes Write fail rather than open latest.log again.
	closed bool
}

// OpenLogFile gzips away a latest.log left by the previous run and opens a
// fresh one. If that roll fails, the error comes back with a log still
// appending to the old latest.log, so lines are lost only when latest.log
// cannot be opened at all.
func OpenLogFile(dir string) (*LogFile, error) {
	return openLogFile(dir, gzipLatest)
}

func openLogFile(dir string, gzip func(string) error) (*LogFile, error) {
	f := &LogFile{dir: dir, now: time.Now, gzip: gzip}
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.roll(); err != nil {
		// The latest.log the roll could not move is still there, so
		// appending to it keeps the lines going until a roll succeeds.
		file, openErr := openLatest(dir)
		if openErr != nil {
			return nil, err
		}
		f.file, f.day = file, f.now()
		return f, err
	}
	return f, nil
}

// Write appends b to latest.log, first rolling the file over when the day
// has turned: log4j's time-based policy triggers on logged lines. A failed
// roll is not retried until the next day-turn.
func (f *LogFile) Write(b []byte) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed {
		return 0, errClosed
	}
	now := f.now()
	if !sameDay(f.day, now) {
		if err := f.roll(); err != nil {
			// Taking the day puts the retry off to the next day-turn.
			f.day = now
			n, writeErr := f.file.Write(b)
			if writeErr != nil {
				return n, writeErr
			}
			return n, err
		}
	}
	return f.file.Write(b)
}

// Close puts the log file down. Safe to call more than once.
func (f *LogFile) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closed = true
	if f.file == nil {
		return nil
	}
	err := f.file.Close()
	f.file = nil
	return err
}

// roll gzips latest.log into the day's archive and opens a fresh one. The
// held file is closed only once the fresh one is open, so a failure in
// between leaves lines going into the old file rather than nowhere.
func (f *LogFile) roll() error {
	if err := f.gzip(f.dir); err != nil {
		return err
	}
	file, err := createLatest(f.dir)
	if err != nil {
		return err
	}
	if f.file != nil {
		_ = f.file.Close()
	}
	f.file, f.day = file, f.now()
	return nil
}

// openLatest opens dir's latest.log for appending, creating dir if needed.
func openLatest(dir string) (*os.File, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	return os.OpenFile(filepath.Join(dir, latestLog), os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o644)
}

// createLatest truncates dir's latest.log rather than removing it, so the
// file the log holds still points at a file on disk if this open fails; the
// old content survives in the archive gzipLatest left.
func createLatest(dir string) (*os.File, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	return os.OpenFile(filepath.Join(dir, latestLog), os.O_WRONLY|os.O_CREATE|os.O_TRUNC|os.O_APPEND, 0o644)
}

// gzipLatest gzips dir's latest.log to <day>-<n>.log.gz, n being the first
// count of that day not taken, and leaves the original for createLatest to
// truncate. A missing or empty latest.log is left alone: vanilla's
// OnStartupTriggeringPolicy has minSize 1.
func gzipLatest(dir string) error {
	path := filepath.Join(dir, latestLog)
	info, err := os.Stat(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.Size() == 0 {
		return nil
	}
	base := filepath.Join(dir, info.ModTime().Format(dayFormat)+"-")
	var target string
	for n := 1; ; n++ {
		target = base + strconv.Itoa(n) + ".log.gz"
		if _, err := os.Stat(target); err != nil {
			break
		}
	}

	old, err := os.Open(path)
	if err != nil {
		return err
	}
	defer old.Close()
	gz, err := os.Create(target)
	if err != nil {
		return err
	}
	w := gzip.NewWriter(gz)
	_, copyErr := io.Copy(w, old)
	gzErr := w.Close()
	if err := gz.Close(); err != nil {
		gzErr = err
	}
	if copyErr != nil || gzErr != nil {
		// the half-written archive is no log of anything
		_ = os.Remove(target)
		if copyErr != nil {
			return copyErr
		}
		return gzErr
	}
	return nil
}

// sameDay compares calendar days through Date, so a logged line costs no
// Format.
func sameDay(a, b time.Time) bool {
	ay, am, ad := a.Date()
	by, bm, bd := b.Date()
	return ay == by && am == bm && ad == bd
}
