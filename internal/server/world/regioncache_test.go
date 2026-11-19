package world

import (
	"errors"
	"testing"
)

// TestRegionCacheEvictsTheLeastRecentlyUsed checks that a cache at its cap
// closes the file used longest ago, not the one opened first, and that
// fetching a cached region keeps a handle rather than reopening it.
func TestRegionCacheEvictsTheLeastRecentlyUsed(t *testing.T) {
	t.Parallel()
	w, err := NewWorld(t.TempDir(), NewFlatGenerator())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = w.Close() })

	handles := make([]*regionFile, maxRegionCache)
	for i := range handles {
		rf, err := w.region(int32(i), 0)
		if err != nil {
			t.Fatal(err)
		}
		handles[i] = rf
		w.releaseRegion(rf)
	}
	if chunks, _ := w.RegionFileCounts(); chunks != maxRegionCache {
		t.Fatalf("open chunk region files = %d, want %d", chunks, maxRegionCache)
	}

	// Touching region 0 makes it the most recently used, so region 1 is the
	// one the next open evicts.
	if rf, err := w.region(0, 0); err != nil {
		t.Fatal(err)
	} else if rf != handles[0] {
		t.Fatal("a cached region was reopened instead of served from the cache")
	} else {
		w.releaseRegion(rf)
	}
	if rf, err := w.region(maxRegionCache, 0); err != nil {
		t.Fatal(err)
	} else {
		w.releaseRegion(rf)
	}

	if rf, err := w.region(1, 0); err != nil {
		t.Fatal(err)
	} else if rf == handles[1] {
		t.Error("region 1 was not evicted; want a freshly opened handle")
	} else {
		w.releaseRegion(rf)
	}
	if rf, err := w.region(0, 0); err != nil {
		t.Fatal(err)
	} else if rf != handles[0] {
		t.Error("region 0 was evicted though it was the most recently used")
	} else {
		w.releaseRegion(rf)
	}
	if chunks, _ := w.RegionFileCounts(); chunks != maxRegionCache {
		t.Errorf("open chunk region files = %d, want %d", chunks, maxRegionCache)
	}
}

// TestAnUnreadableChunkSurvivesRegionEviction checks that the mark a chunk
// gets when its data fails to read outlives the eviction of its region file:
// reopening the file must not clear it and let a write overwrite the damaged
// chunk.
func TestAnUnreadableChunkSurvivesRegionEviction(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	w, err := NewWorld(dir, NewFlatGenerator())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = w.Close() })

	pos := ChunkPos{}
	damageChunk(t, regionFilePath(w.storage, 0, 0), pos)
	if _, err := w.loadChunk(pos); err == nil {
		t.Fatal("loadChunk of the damaged chunk succeeded")
	}

	// Open enough other regions to force region (0,0) out of the cache.
	for i := int32(1); i <= maxRegionCache; i++ {
		rf, err := w.region(i, 0)
		if err != nil {
			t.Fatal(err)
		}
		w.releaseRegion(rf)
	}

	rf, err := w.region(0, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer w.releaseRegion(rf)
	if err := rf.writeChunk(pos, []byte("a regenerated chunk")); !errors.Is(err, errUnreadable) {
		t.Errorf("writeChunk over the damaged chunk after eviction = %v, want errUnreadable", err)
	}
}
