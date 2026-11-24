package world

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/golem-mc/golem/nbt"
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
	c.finish(pos, version, map[posKey]bool{key: true})
	if _, ok := c.get(pos, key); ok {
		t.Error("a read that crossed an invalidation was published")
	}
	if len(c.pending) != 0 {
		t.Errorf("%d reads still pending", len(c.pending))
	}

	c.finish(pos, c.begin(pos), map[posKey]bool{key: true})
	if found, ok := c.get(pos, key); !ok || !found {
		t.Errorf("get after a clean read = %v, %v, want true, true", found, ok)
	}

	c.drop(pos)
	for i := range lodestoneCacheMax {
		p := ChunkPos{X: int32(i + 10)}
		c.finish(p, c.begin(p), map[posKey]bool{{p.X << 4, 64, 0}: false})
	}
	if c.entries != lodestoneCacheMax {
		t.Fatalf("entries = %d, want the cap %d", c.entries, lodestoneCacheMax)
	}
	extra := ChunkPos{X: 5000}
	c.finish(extra, c.begin(extra), map[posKey]bool{{extra.X << 4, 64, 0}: true})
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

// savedLodestoneRoot is a saved chunk (0,0) root with one section at Y 4 whose
// palette is the given block names, the first filling it.
func savedLodestoneRoot(t *testing.T, status string, names ...string) nbt.Compound {
	t.Helper()
	root, err := encodeChunkNBT(newChunk(0, 0))
	if err != nil {
		t.Fatal(err)
	}
	var palette []nbt.Tag
	for _, name := range names {
		palette = append(palette, nbt.Compound{"Name": nbt.String(name)})
	}
	root["Status"] = nbt.String(status)
	root["sections"] = nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{nbt.Compound{
		"Y":            nbt.Byte(4),
		"block_states": nbt.Compound{"palette": nbt.List{Elem: nbt.TypeCompound, Items: palette}},
	}}}
	return root
}

// writeSavedRoot stores root as chunk (0,0) of a new world's overworld.
func writeSavedRoot(t *testing.T, root nbt.Compound) *World {
	t.Helper()
	dir := t.TempDir()
	data, err := nbt.Encode("", root)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(DimensionDir(dir, DimensionName), "region"), 0o755); err != nil {
		t.Fatal(err)
	}
	rf, err := openRegion(regionFilePath(DimensionDir(dir, DimensionName), 0, 0), defaultSaveFormat)
	if err != nil {
		t.Fatal(err)
	}
	if err := rf.writeChunk(ChunkPos{}, data); err != nil {
		t.Fatal(err)
	}
	if err := rf.close(); err != nil {
		t.Fatal(err)
	}
	w, err := NewWorld(dir, NewFlatGenerator())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = w.Close() })
	return w
}

// TestResolveLodestonesBatch checks positions of one saved section are
// answered by one read, kept for the next ask.
func TestResolveLodestonesBatch(t *testing.T) {
	w := writeSavedRoot(t, savedLodestoneRoot(t, "minecraft:full", "minecraft:lodestone"))
	in, out := LodestonePos{1, 64, 1}, LodestonePos{1, 200, 1}
	got := w.ResolveLodestones([]LodestonePos{in, {2, 70, 3}, out})
	if !got[in] || !got[LodestonePos{2, 70, 3}] {
		t.Errorf("answers %v: a lodestone in the filled section was missed", got)
	}
	if present, ok := got[out]; !ok || present {
		t.Errorf("answer for a position in a section with no record = %v, %v, want false, true", present, ok)
	}
	if present, known := w.LodestoneCached(in.X, in.Y, in.Z); !known || !present {
		t.Errorf("LodestoneCached after the batch = %v, %v, want true, true", present, known)
	}
}

// TestResolveLodestonesKeepsUnreadableUnknown checks a record that does not
// decode leaves its positions unanswered and uncached, and the chunk writable.
func TestResolveLodestonesKeepsUnreadableUnknown(t *testing.T) {
	w := writeSavedRoot(t, savedLodestoneRoot(t, "minecraft:full", "minecraft:not_a_block"))
	p := LodestonePos{1, 64, 1}
	if got := w.ResolveLodestones([]LodestonePos{p}); len(got) != 0 {
		t.Errorf("answers %v for a record that does not decode, want none", got)
	}
	if _, known := w.LodestoneCached(p.X, p.Y, p.Z); known {
		t.Error("a failed read was cached")
	}
	rx, rz := regionCoords(ChunkPos{})
	rf, err := w.region(rx, rz)
	if err != nil {
		t.Fatal(err)
	}
	defer w.releaseRegion(rf)
	if rf.unreadable[regionIndex(0, 0)] {
		t.Error("the compass lookup marked the chunk unreadable")
	}
}

// TestResolveLodestonesSkipsNonFullRecord checks a record loading would skip
// answers absent, and is left unmarked.
func TestResolveLodestonesSkipsNonFullRecord(t *testing.T) {
	w := writeSavedRoot(t, savedLodestoneRoot(t, "minecraft:carved", "minecraft:lodestone"))
	p := LodestonePos{1, 64, 1}
	if present, ok := w.ResolveLodestones([]LodestonePos{p})[p]; !ok || present {
		t.Errorf("answer = %v, %v, want false, true", present, ok)
	}
}

// BenchmarkResolveLodestones is the cost of finding out whether a saved chunk
// holds a lodestone with nothing known.
func BenchmarkResolveLodestones(b *testing.B) {
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
		if !w.ResolveLodestones([]LodestonePos{{50, 63, 70}})[LodestonePos{50, 63, 70}] {
			b.Fatal("the saved lodestone was not found")
		}
	}
}
