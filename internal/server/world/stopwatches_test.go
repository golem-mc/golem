package world

import (
	"testing"
	"time"
)

// TestStopwatchKeepsTimeAcrossARestart checks that a saved stopwatch goes on
// running across a restart before anything names it: a fresh Stopwatches loads
// it at once, so the wait between the restart and the query counts, and a save
// that never named a stopwatch still records the time it ran.
func TestStopwatchKeepsTimeAcrossARestart(t *testing.T) {
	t.Parallel()
	root := t.TempDir()

	saved := NewStopwatches(root)
	if !saved.Create("foo:bar") {
		t.Fatal("create foo:bar: already exists")
	}
	saved.Save()

	// A fresh server, as after a restart, pointed at the same world: the
	// stopwatch must resume from the saved time as soon as it is built.
	restarted := NewStopwatches(root)
	time.Sleep(1500 * time.Millisecond)
	if seconds, ok := restarted.ElapsedSeconds("foo:bar"); !ok || seconds < 1.4 {
		t.Errorf("a restarted stopwatch read %v (%v s), want the wait counted", ok, seconds)
	}

	// The restart's own save, which named no stopwatch, must still have
	// written the time it ran.
	restarted.Save()
	again := NewStopwatches(root)
	if seconds, ok := again.ElapsedSeconds("foo:bar"); !ok || seconds < 1.4 {
		t.Errorf("the untouched save recorded %v (%v s), want the wait kept", ok, seconds)
	}
}
