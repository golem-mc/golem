package world

import (
	"runtime"
	"testing"

	"github.com/golem-mc/golem/registry"
)

// TestLodestoneCacheFillCrossingInvalidation checks a read begun before a load
// or write of its chunk does not publish its answer, and that a full cache
// gives up one answer instead of all.
func TestLodestoneCacheFillCrossingInvalidation(t *testing.T) {
	var c lodestoneCache
	pos := ChunkPos{X: 1, Z: 2}
	key := posKey{16, 64, 32}

	version := c.begin(pos)
	c.drop(pos) // the chunk loaded, or was written, while the disk was read
	c.finish(pos, key, version, true)
	if _, ok := c.get(pos, key); ok {
		t.Error("a read that crossed an invalidation was published")
	}
	if len(c.pending) != 0 {
		t.Errorf("%d reads still pending", len(c.pending))
	}

	c.finish(pos, key, c.begin(pos), true)
	if found, ok := c.get(pos, key); !ok || !found {
		t.Errorf("get after a clean read = %v, %v, want true, true", found, ok)
	}

	c.drop(pos)
	for i := range lodestoneCacheMax {
		p := ChunkPos{X: int32(i + 10)}
		c.finish(p, posKey{p.X << 4, 64, 0}, c.begin(p), false)
	}
	if c.entries != lodestoneCacheMax {
		t.Fatalf("entries = %d, want the cap %d", c.entries, lodestoneCacheMax)
	}
	extra := ChunkPos{X: 5000}
	c.finish(extra, posKey{extra.X << 4, 64, 0}, c.begin(extra), true)
	if c.entries != lodestoneCacheMax {
		t.Errorf("entries after an admission to a full cache = %d, want %d", c.entries, lodestoneCacheMax)
	}
	if _, ok := c.get(extra, posKey{extra.X << 4, 64, 0}); !ok {
		t.Error("the new answer was not kept")
	}
	kept := 0
	for i := range lodestoneCacheMax {
		p := ChunkPos{X: int32(i + 10)}
		if _, ok := c.get(p, posKey{p.X << 4, 64, 0}); ok {
			kept++
		}
	}
	if kept != lodestoneCacheMax-1 {
		t.Errorf("kept %d of the older answers, want %d", kept, lodestoneCacheMax-1)
	}
}

// BenchmarkResolveLodestone is the cost of finding out whether a saved chunk
// holds a lodestone with nothing known, the work a compass tick pays once per
// target.
func BenchmarkResolveLodestone(b *testing.B) {
	w, _ := NewWorld(b.TempDir(), NewOverworldGenerator(5))
	b.Cleanup(func() { _ = w.Close() })
	pos := ChunkPos{X: 3, Z: 4}
	w.Acquire(pos)
	w.SetBlock(50, 63, 70, registry.BlockLodestone.DefaultState())
	w.Save()
	w.Release(pos)
	for w.ChunkLoaded(pos) {
		runtime.Gosched()
	}
	b.ReportAllocs()
	for b.Loop() {
		w.lodestones = lodestoneCache{}
		if !w.ResolveLodestone(50, 63, 70) {
			b.Fatal("the saved lodestone was not found")
		}
	}
}
