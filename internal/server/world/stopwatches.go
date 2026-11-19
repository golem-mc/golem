package world

import (
	"errors"
	"log/slog"
	"slices"
	"sync"
	"time"

	"github.com/golem-mc/golem/nbt"
)

// stopwatchesFile is /stopwatch's file: a compound of id to milliseconds run.
// vanilla: Stopwatches
const stopwatchesFile = "data/minecraft/stopwatches.dat"

// stopwatchEpoch is the process start the stopwatches measure from; time.Since
// reads the monotonic clock, so a system clock step does not move them.
// vanilla: Util.getMillis
var stopwatchEpoch = time.Now()

// stopwatchMillis is the process's running time in milliseconds, monotonic.
func stopwatchMillis() int64 { return time.Since(stopwatchEpoch).Milliseconds() }

// stopwatchState is one stopwatch: the process millisecond it was created or
// restarted at, and the milliseconds it had run before then, so one read from
// disk resumes with its saved time. It is measured on the monotonic clock, not
// ticks, so a paused or lagging server does not affect it.
// vanilla: Stopwatch
type stopwatchState struct {
	startedAt int64
	elapsed   int64
}

// elapsedMilliseconds is how long the stopwatch has run by now.
func (s stopwatchState) elapsedMilliseconds(now int64) int64 {
	return s.elapsed + now - s.startedAt
}

// Stopwatches is vanilla's Stopwatches saved data: id to running stopwatch,
// read from data/minecraft/stopwatches.dat at start and written by later saves.
// vanilla: Stopwatches
type Stopwatches struct {
	root string

	mu      sync.Mutex
	entries map[string]stopwatchState
	dirty   bool
}

// NewStopwatches loads root's saved stopwatches, which continue from where they
// stopped.
func NewStopwatches(root string) *Stopwatches {
	s := &Stopwatches{root: root, entries: map[string]stopwatchState{}}
	s.load()
	return s
}

// Create starts a stopwatch from now, false if the id is taken.
func (s *Stopwatches) Create(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.entries[id]; ok {
		return false
	}
	s.entries[id] = stopwatchState{startedAt: stopwatchMillis()}
	s.dirty = true
	return true
}

// ElapsedSeconds is the stopwatch's elapsed seconds, false if none.
func (s *Stopwatches) ElapsedSeconds(id string) (float64, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	w, ok := s.entries[id]
	if !ok {
		return 0, false
	}
	return float64(w.elapsedMilliseconds(stopwatchMillis())) / 1000, true
}

// Restart resets the stopwatch and runs it from now, false if none.
func (s *Stopwatches) Restart(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.entries[id]; !ok {
		return false
	}
	s.entries[id] = stopwatchState{startedAt: stopwatchMillis()}
	s.dirty = true
	return true
}

// Remove drops the named stopwatch, answering false when there is none.
func (s *Stopwatches) Remove(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.entries[id]; !ok {
		return false
	}
	delete(s.entries, id)
	s.dirty = true
	return true
}

// IDs lists the stopwatch ids in order, for suggestions.
func (s *Stopwatches) IDs() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	ids := make([]string, 0, len(s.entries))
	for id := range s.entries {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	return ids
}

// Save writes stopwatches.dat while any exist or one was removed since the last
// save; it stores elapsed milliseconds at the save, from which a later server
// resumes.
func (s *Stopwatches) Save() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.dirty && len(s.entries) == 0 {
		return
	}
	now := stopwatchMillis()
	stored := make(nbt.Compound, len(s.entries))
	for id, w := range s.entries {
		stored[id] = nbt.Long(w.elapsedMilliseconds(now))
	}
	if err := writeSavedData(s.root, stopwatchesFile, nbt.Compound{"stopwatches": stored}); err != nil {
		slog.Error("could not save the stopwatches", "error", err)
		return
	}
	s.dirty = false
}

// load seeds each entry with its saved elapsed time, running from now. No lock:
// it runs before the stopwatches are shared.
func (s *Stopwatches) load() {
	data, err := readSavedData(s.root, stopwatchesFile)
	if errors.Is(err, errNoSavedData) {
		return
	} else if err != nil {
		slog.Warn("could not read stopwatches.dat, using none", "error", err)
		return
	}
	stored, ok := nbt.Get[nbt.Compound](data, "stopwatches")
	if !ok {
		return
	}
	now := stopwatchMillis()
	for id, tag := range stored {
		ms, ok := nbt.LongValue(tag)
		if !ok {
			continue
		}
		s.entries[id] = stopwatchState{startedAt: now, elapsed: ms}
	}
}
