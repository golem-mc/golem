package world

import (
	"bytes"
	"runtime"
	"sync"
	"testing"
	"time"
)

// parkedSaves counts the chunk saves the I/O pool has parked: on a region
// file's write lock, or on a chunk's own save lock once saves of one chunk are
// serialised.
func parkedSaves() int {
	buf := make([]byte, 1<<20)
	dump := buf[:runtime.Stack(buf, true)]
	return bytes.Count(dump, []byte("world.(*regionFile).writeChunk(")) +
		bytes.Count(dump, []byte("world.(*World).saveLoaded("))
}

func waitParkedSaves(t *testing.T, want int) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for {
		if parkedSaves() >= want {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("the I/O pool never parked %d saves", want)
		}
		time.Sleep(200 * time.Microsecond)
	}
}

// widenIOPool widens the I/O pool so two saves can race. One I/O worker is too
// few on a two-core machine for that; a wider machine runs more.
func widenIOPool(w *World) {
	w.exec.ioSem = make(chan struct{}, 4)
}

// parkRelease holds the region file's write lock so the saves the next
// Releases submit park after encoding. The returned count is the parked
// baseline, and unpark lets them write. unpark also runs on cleanup: the
// caller registers w.Close as a cleanup before this, so a failed wait runs
// unpark first and Close does not block on the parked save.
func parkRelease(t *testing.T, w *World, pos ChunkPos) (baseline int, unpark func()) {
	t.Helper()
	rx, rz := regionCoords(pos)
	rf, err := w.region(rx, rz)
	if err != nil {
		t.Fatal(err)
	}
	baseline = parkedSaves()
	rf.mu.Lock()
	var once sync.Once
	unpark = func() { once.Do(rf.mu.Unlock) }
	t.Cleanup(unpark)
	return baseline, unpark
}

// TestAnOlderUnloadSaveKeepsANewerChange parks an unload save after it has
// encoded an older chunk, writes a newer state into that same copy, then lets
// the save write. The older save must not unload the chunk from memory: if it
// does, the next acquire reloads its stale bytes over the newer change.
func TestAnOlderUnloadSaveKeepsANewerChange(t *testing.T) {
	older := rsState(t, "minecraft:diamond_block", nil)
	newer := rsState(t, "minecraft:gold_block", nil)
	w, _ := NewWorld(t.TempDir(), NewFlatGenerator())
	t.Cleanup(func() { _ = w.Close() })
	pos := ChunkPos{}

	_, _ = w.Acquire(pos)
	w.SetBlock(5, GroundLevel-1, 3, older)
	// Park the save after it encoded the older chunk but before it wrote.
	widenIOPool(w)
	baseline, unpark := parkRelease(t, w, pos)
	w.Release(pos)
	waitParkedSaves(t, baseline+1)
	// The chunk is still in memory, so this lands on the copy the save
	// encoded — a newer generation with no Release to save it. A save
	// holding the older generation must not unload that copy.
	w.SetBlock(5, GroundLevel-1, 3, newer)
	unpark()
	w.FlushSaves()

	again, _ := w.Acquire(pos)
	if got := again.BlockAt(5, GroundLevel-1, 3); got != newer {
		t.Fatalf("region holds block %d, want %d", got, newer)
	}
	w.Release(pos)
}
