package world

import (
	"errors"
	"strings"
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

// TestRegionCachesAreBounded checks that visiting more regions than the cap
// leaves each store at the cap rather than growing a handle for every region
// visited, the chunks' cache and the entities' alike.
func TestRegionCachesAreBounded(t *testing.T) {
	t.Parallel()
	w, err := NewWorld(t.TempDir(), NewFlatGenerator())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = w.Close() })

	const visited = maxRegionCache + 44
	for i := range int32(visited) {
		if rf, err := w.region(i, 0); err != nil {
			t.Fatal(err)
		} else {
			w.releaseRegion(rf)
		}
		if rf, err := w.entityRegion(i, 0); err != nil {
			t.Fatal(err)
		} else {
			w.releaseEntityRegion(rf)
		}
	}
	chunks, entities := w.RegionFileCounts()
	if chunks != maxRegionCache {
		t.Errorf("open chunk region files = %d after %d regions, want %d", chunks, visited, maxRegionCache)
	}
	if entities != maxRegionCache {
		t.Errorf("open entity region files = %d after %d regions, want %d", entities, visited, maxRegionCache)
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

// TestAnUnreadableEntityChunkSurvivesRegionEviction is the entity store's form
// of TestAnUnreadableChunkSurvivesRegionEviction: the mark a chunk's entities
// get when they fail to read must outlive the file's eviction.
func TestAnUnreadableEntityChunkSurvivesRegionEviction(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	w, err := NewWorld(dir, NewFlatGenerator())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = w.Close() })

	pos := ChunkPos{}
	damageChunk(t, entitiesRegionFilePath(w.storage, 0, 0), pos)
	if _, _, err := w.LoadChunkEntities(pos); err == nil {
		t.Fatal("LoadChunkEntities of the damaged chunk succeeded")
	}

	// Open enough other entity regions to force region (0,0) out of the cache.
	for i := int32(1); i <= maxRegionCache; i++ {
		rf, err := w.entityRegion(i, 0)
		if err != nil {
			t.Fatal(err)
		}
		w.releaseEntityRegion(rf)
	}

	rf, err := w.entityRegion(0, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer w.releaseEntityRegion(rf)
	if err := rf.writeChunk(pos, []byte("regenerated entities")); !errors.Is(err, errUnreadable) {
		t.Errorf("writeChunk over the damaged entity chunk after eviction = %v, want errUnreadable", err)
	}
}

// TestRegionHandleStaysOpenWhileOthersAreOpened checks that a handle taken
// from the cache is not closed when opening more regions evicts its entry: a
// read or write still running on it must not fail with a closed file.
func TestRegionHandleStaysOpenWhileOthersAreOpened(t *testing.T) {
	t.Parallel()
	w, err := NewWorld(t.TempDir(), NewFlatGenerator())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = w.Close() })

	rf, err := w.region(0, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer w.releaseRegion(rf)

	for i := int32(1); i <= maxRegionCache; i++ {
		other, err := w.region(i, 0)
		if err != nil {
			t.Fatal(err)
		}
		w.releaseRegion(other)
	}

	if err := rf.writeChunk(ChunkPos{}, []byte("a chunk written while held")); err != nil {
		t.Errorf("writeChunk on a held handle = %v, want nil", err)
	}
}

// TestRegionCloseErrorsAreReturned checks that a region file whose handle was
// closed underneath the cache makes the cache's close, and the world's, fail
// with an error naming that file instead of dropping it.
func TestRegionCloseErrorsAreReturned(t *testing.T) {
	t.Parallel()
	w, err := NewWorld(t.TempDir(), NewFlatGenerator())
	if err != nil {
		t.Fatal(err)
	}

	rf, err := w.region(3, -2)
	if err != nil {
		t.Fatal(err)
	}
	w.releaseRegion(rf)
	if err := rf.file.Close(); err != nil {
		t.Fatal(err)
	}

	err = w.Close()
	if err == nil {
		t.Fatal("World.Close after a region file was closed underneath it = nil, want an error")
	}
	if !strings.Contains(err.Error(), rf.path) {
		t.Errorf("World.Close error = %q, want it to name %s", err, rf.path)
	}
	if err := w.Close(); err != nil {
		t.Errorf("second World.Close = %v, want nil", err)
	}
}

// TestRegionCacheCloseNamesTheFailingFile is the cache-level form of
// TestRegionCloseErrorsAreReturned.
func TestRegionCacheCloseNamesTheFailingFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	c := newRegionCache(func(rx, rz int32) (*regionFile, error) {
		return openRegion(regionFilePath(dir, rx, rz), defaultSaveFormat)
	})
	rf, err := c.get(0, 0)
	if err != nil {
		t.Fatal(err)
	}
	c.release(rf)
	if err := rf.file.Close(); err != nil {
		t.Fatal(err)
	}
	if err := c.close(); err == nil || !strings.Contains(err.Error(), rf.path) {
		t.Errorf("regionCache.close = %v, want an error naming %s", err, rf.path)
	}
}
