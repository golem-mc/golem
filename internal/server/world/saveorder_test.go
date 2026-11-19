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

// TestSaveSharesTheChunkSaveOrder pins the fix for an autosave racing an unload
// save: Save has to write through the chunk's own save lock and generation, so
// it cannot land an older generation after a newer unload save. The two writes
// are ordered by the region lock in practice, so assert the shared generation
// rather than a timing-dependent interleaving.
func TestSaveSharesTheChunkSaveOrder(t *testing.T) {
	w, _ := NewWorld(t.TempDir(), NewFlatGenerator())
	defer w.Close()
	pos := ChunkPos{}
	_, _ = w.Acquire(pos)
	w.SetBlock(5, GroundLevel-1, 3, rsState(t, "minecraft:diamond_block", nil))
	w.mu.Lock()
	l := w.chunks[pos]
	gen := l.dirtyGen
	w.mu.Unlock()

	w.Save()

	l.saveMu.Lock()
	saved := l.savedGen
	l.saveMu.Unlock()
	if saved != gen {
		t.Fatalf("Save recorded generation %d, want %d", saved, gen)
	}
}

// TestUnloadSavesOfAChunkStayOrdered pushes unload saves of one chunk through
// the I/O pool so an older save encodes before the newer change and they all
// then wait on the same region write. The region file has to end up holding
// the newer save's bytes; without a per-chunk order an older save can land
// last and the change is lost on the next load.
func TestUnloadSavesOfAChunkStayOrdered(t *testing.T) {
	older := rsState(t, "minecraft:diamond_block", nil)
	newer := rsState(t, "minecraft:gold_block", nil)
	w, _ := NewWorld(t.TempDir(), NewFlatGenerator())
	t.Cleanup(func() { _ = w.Close() })
	widenIOPool(w)
	for attempt := range 150 {
		pos := ChunkPos{X: int32(attempt % 32), Z: 0}
		worldX := pos.X*16 + 5
		// A state neither save writes, so the first edit is never a no-op
		// after an earlier attempt left the chunk's newer state in the file.
		_, _ = w.Acquire(pos)
		w.SetBlock(worldX, GroundLevel-1, 3, stoneState)
		w.SetBlock(worldX, GroundLevel-1, 3, older)

		// Hold the region's write lock so the saves park after encoding;
		// an older save encodes first, the newer change lands while it
		// waits, and which save the lock wakes first is the race.
		baseline, unpark := parkRelease(t, w, pos)
		w.Release(pos)
		_, _ = w.Acquire(pos)
		w.Release(pos)
		waitParkedSaves(t, baseline+2)
		_, _ = w.Acquire(pos)
		w.SetBlock(worldX, GroundLevel-1, 3, newer)
		w.Release(pos)
		waitParkedSaves(t, baseline+3)
		unpark()
		w.FlushSaves()

		again, _ := w.Acquire(pos)
		if got := again.BlockAt(5, GroundLevel-1, 3); got != newer {
			t.Fatalf("attempt %d: region holds block %d, want %d (the newer save's bytes)",
				attempt, got, newer)
		}
		w.Release(pos)
	}
}

// TestAnAcquireDuringAReleaseSaveGetsTheLiveChunk re-acquires a chunk while its
// release save is parked. The chunk is still in the map, so the acquire must
// hand back the live copy; reloading it from disk would serve the bytes from
// before the save, and a later unload would then strand the re-acquired chunk.
func TestAnAcquireDuringAReleaseSaveGetsTheLiveChunk(t *testing.T) {
	block := rsState(t, "minecraft:diamond_block", nil)
	w, _ := NewWorld(t.TempDir(), NewFlatGenerator())
	t.Cleanup(func() { _ = w.Close() })
	pos := ChunkPos{}

	_, _ = w.Acquire(pos)
	w.SetBlock(5, GroundLevel-1, 3, block)
	w.mu.RLock()
	live := w.chunks[pos].chunk
	w.mu.RUnlock()

	widenIOPool(w)
	baseline, unpark := parkRelease(t, w, pos)
	w.Release(pos)
	waitParkedSaves(t, baseline+1)
	again, _ := w.Acquire(pos)
	if again != live {
		t.Fatal("the acquire during the release save returned a chunk reloaded from disk")
	}
	if got := again.BlockAt(5, GroundLevel-1, 3); got != block {
		t.Fatalf("the live chunk holds block %d, want %d", got, block)
	}
	unpark()
	w.FlushSaves()

	// The save must not unload the chunk the acquire took back into use.
	if !w.ChunkLoaded(pos) {
		t.Fatal("the chunk was unloaded after being re-acquired during its save")
	}
	w.Release(pos)
}
