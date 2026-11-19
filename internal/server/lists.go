package server

import (
	"encoding/json"
	"log/slog"
	"os"
	"sync"
	"time"
)

// listTimeLayout is vanilla's SimpleDateFormat "yyyy-MM-dd HH:mm:ss Z" for
// list entries; listExpiresNever marks a ban that never lapses.
const (
	listTimeLayout   = "2006-01-02 15:04:05 -0700"
	listExpiresNever = "forever"
)

// listTime is the created stamp an entry added now carries.
func listTime() string { return time.Now().Format(listTimeLayout) }

// jsonList is one of vanilla's working-directory JSON lists (ops.json,
// banned-players.json, banned-ips.json, whitelist.json): one array read on
// first use and rewritten whole on every change.
type jsonList[T any] struct {
	mu      sync.Mutex
	path    string
	loaded  bool
	exists  bool
	entries []T
}

// newJSONList keeps the list at path.
func newJSONList[T any](path string) *jsonList[T] { return &jsonList[T]{path: path} }

// load reads the list from disk if it has not been read yet.
func (l *jsonList[T]) load() {
	if l.loaded {
		return
	}
	l.loaded = true
	data, err := os.ReadFile(l.path)
	if err != nil {
		if !os.IsNotExist(err) {
			slog.Warn("could not read list file", "path", l.path, "error", err)
		}
		l.exists = false
		l.entries = nil
		return
	}
	l.exists = true
	if err := json.Unmarshal(data, &l.entries); err != nil {
		slog.Warn("could not parse list file", "path", l.path, "error", err)
		l.entries = nil
	}
}

// snapshot returns a copy of the entries.
func (l *jsonList[T]) snapshot() []T {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.load()
	return append([]T(nil), l.entries...)
}

// setFile points the list at another path, for tests.
func (l *jsonList[T]) setFile(path string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.path = path
	l.loaded = false
	l.exists = false
	l.entries = nil
}

// file reports where the list is kept.
func (l *jsonList[T]) file() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.path
}

// hasFile reports whether the list file exists, which lists created only on
// first write count by.
func (l *jsonList[T]) hasFile() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.load()
	return l.exists
}

// reload drops the reading so that the next read comes from disk again.
func (l *jsonList[T]) reload() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.loaded = false
}

// update rewrites the list with f's entries, saving only if f reports a
// change.
func (l *jsonList[T]) update(f func([]T) ([]T, bool)) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.load()
	entries, changed := f(l.entries)
	l.entries = entries
	if changed {
		l.write()
	}
}

// write rewrites the whole array.
func (l *jsonList[T]) write() {
	entries := l.entries
	if entries == nil {
		entries = []T{}
	}
	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		slog.Error("could not encode list file", "path", l.path, "error", err)
		return
	}
	if err := os.WriteFile(l.path, data, 0o644); err != nil {
		slog.Error("could not write list file", "path", l.path, "error", err)
		return
	}
	l.exists = true
}
