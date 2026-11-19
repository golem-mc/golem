package world

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
)

// captureErrors routes slog to a buffer for the test.
func captureErrors(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(old) })
	return &buf
}

// chainPhysics is a one-chunk engine; it is not parallel because the tests
// swap the process-wide logger.
func chainPhysics(t *testing.T) (*World, *Physics) {
	t.Helper()
	w := newTestWorld(t, NewFlatGenerator())
	t.Cleanup(func() { w.Close() })
	w.Acquire(ChunkPos{})
	return w, NewPhysics(w)
}

// nest runs depth nested updates the way a chain does, returning how many ran.
func nest(p *Physics, depth int) (ran int) {
	var rec func(i int)
	rec = func(i int) {
		if i == depth {
			return
		}
		if !p.chainStart(int32(i), 64, 0) {
			return
		}
		ran++
		rec(i + 1)
		p.chainEnd()
	}
	rec(0)
	return ran
}

func TestChainedNeighborUpdateCap(t *testing.T) {
	_, p := chainPhysics(t)
	logs := captureErrors(t)
	p.SetMaxChainedNeighborUpdates(10)
	if got := nest(p, 25); got != 10 {
		t.Fatalf("ran %d updates, want 10", got)
	}
	if n := strings.Count(logs.String(), "Too many chained neighbor updates"); n != 1 {
		t.Fatalf("logged %d times, want 1: %s", n, logs)
	}
	if !strings.Contains(logs.String(), "First skipped position: 10, 64, 0") {
		t.Fatalf("wrong first skipped position: %s", logs)
	}
	// The count resets once the outermost update finishes.
	if got := nest(p, 5); got != 5 {
		t.Fatalf("second chain ran %d, want 5", got)
	}
	p.SetMaxChainedNeighborUpdates(-1)
	logs.Reset()
	if got := nest(p, 25); got != 25 || logs.Len() != 0 {
		t.Fatalf("unlimited ran %d, logged %q", got, logs)
	}
}

// TestChainedNeighborUpdateFanOut drives the real fan-out: the position and
// its six neighbours are siblings of one chain, so a cap of 3 trips once.
func TestChainedNeighborUpdateFanOut(t *testing.T) {
	_, p := chainPhysics(t)
	logs := captureErrors(t)
	p.SetMaxChainedNeighborUpdates(3)
	var changes []BlockChange
	p.neighborUpdate(8, GroundLevel+1, 8, 0, 0, &changes)
	if n := strings.Count(logs.String(), "Too many chained neighbor updates"); n != 1 {
		t.Fatalf("logged %d times, want 1: %s", n, logs)
	}
	if p.chainDepth != 0 || p.chainCount != 0 {
		t.Fatalf("chain not reset: depth %d count %d", p.chainDepth, p.chainCount)
	}
	logs.Reset()
	p.SetMaxChainedNeighborUpdates(-1)
	p.neighborUpdate(8, GroundLevel+1, 8, 0, 0, &changes)
	if logs.Len() != 0 {
		t.Fatalf("unlimited logged %q", logs)
	}
}
