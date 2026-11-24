package world

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

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
	if c.valid(pos, version) {
		t.Error("a read that crossed an invalidation is still valid")
	}
	c.publish(pos, version, map[posKey]bool{key: true})
	c.end(pos)
	if _, ok := c.get(pos, key); ok {
		t.Error("a read that crossed an invalidation was published")
	}
	if len(c.pending) != 0 {
		t.Errorf("%d reads still pending", len(c.pending))
	}

	c.fill(pos, map[posKey]bool{key: true})
	if found, ok := c.get(pos, key); !ok || !found {
		t.Errorf("get after a clean read = %v, %v, want true, true", found, ok)
	}

	c.drop(pos)
	for i := range lodestoneCacheMax {
		p := ChunkPos{X: int32(i + 10)}
		c.fill(p, map[posKey]bool{{p.X << 4, 64, 0}: false})
	}
	if c.entries != lodestoneCacheMax {
		t.Fatalf("entries = %d, want the cap %d", c.entries, lodestoneCacheMax)
	}
	extra := ChunkPos{X: 5000}
	c.fill(extra, map[posKey]bool{{extra.X << 4, 64, 0}: true})
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

// fill is a clean read of the chunk that found answers.
func (c *lodestoneCache) fill(pos ChunkPos, answers map[posKey]bool) {
	c.publish(pos, c.begin(pos), answers)
	c.end(pos)
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
	defer got.Close()
	if a, _ := got.Lookup(in); !a {
		t.Error("a lodestone in the filled section was missed")
	}
	if a, _ := got.Lookup(LodestonePos{2, 70, 3}); !a {
		t.Error("a second lodestone in the filled section was missed")
	}
	if present, ok := got.Lookup(out); !ok || present {
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
	got := w.ResolveLodestones([]LodestonePos{p})
	defer got.Close()
	if _, known := got.Lookup(p); known {
		t.Error("answered for a record that does not decode")
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
	got := w.ResolveLodestones([]LodestonePos{p})
	defer got.Close()
	if present, ok := got.Lookup(p); !ok || present {
		t.Errorf("answer = %v, %v, want false, true", present, ok)
	}
}

// TestResolveLodestonesLeavesRegionFailureUnmarked checks a record whose
// compression cannot be read leaves positions unanswered and the chunk
// writable.
func TestResolveLodestonesLeavesRegionFailureUnmarked(t *testing.T) {
	w := writeSavedRoot(t, savedLodestoneRoot(t, "minecraft:full", "minecraft:lodestone"))
	rx, rz := regionCoords(ChunkPos{})
	path := regionFilePath(w.storage, rx, rz)
	f, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	rf, err := w.region(rx, rz)
	if err != nil {
		t.Fatal(err)
	}
	defer w.releaseRegion(rf)
	sector := int64(rf.offsets[regionIndex(0, 0)] >> 8)
	if _, err := f.WriteAt([]byte{99}, sector*sectorSize+4); err != nil { // no such compression type
		t.Fatal(err)
	}
	f.Close()
	p := LodestonePos{1, 64, 1}
	got := w.ResolveLodestones([]LodestonePos{p})
	defer got.Close()
	if _, known := got.Lookup(p); known {
		t.Error("answered for a record that does not decompress")
	}
	if rf.unreadable[regionIndex(0, 0)] {
		t.Error("the compass lookup marked the chunk unreadable")
	}
}

// TestResolveLodestonesRefusesTruncatedRegion checks a region file too short
// for its header leaves the position unknown and uncached, and the file as it
// was, and that the chunk is not read again until the retry delay passes,
// when the repaired file answers.
func TestResolveLodestonesRefusesTruncatedRegion(t *testing.T) {
	w := writeSavedRoot(t, savedLodestoneRoot(t, "minecraft:full", "minecraft:lodestone"))
	rx, rz := regionCoords(ChunkPos{})
	path := regionFilePath(w.storage, rx, rz)
	saved, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, saved[:4], 0o644); err != nil {
		t.Fatal(err)
	}
	now := time.Unix(1000, 0)
	w.lodestones.now = func() time.Time { return now }
	p := LodestonePos{1, 64, 1}
	resolve := func() (present, known bool) {
		got := w.ResolveLodestones([]LodestonePos{p})
		defer got.Close()
		return got.Lookup(p)
	}
	if _, known := resolve(); known {
		t.Error("answered for a truncated region file")
	}
	if _, known := w.LodestoneCached(p.X, p.Y, p.Z); known {
		t.Error("a truncated region file was cached as an answer")
	}
	if st, err := os.Stat(path); err != nil || st.Size() != 4 {
		t.Fatalf("region file after the lookup: %v, %v; want it left at 4 bytes", st, err)
	}

	if err := os.WriteFile(path, saved, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, known := resolve(); known {
		t.Error("the failed chunk was read again before the retry delay")
	}
	now = now.Add(lodestoneRetryDelay)
	if present, known := resolve(); !known || !present {
		t.Errorf("answer after the retry delay = %v, %v, want true, true", present, known)
	}
}

// TestLodestoneRetryClearedByLoadOrWrite checks a load or write of a chunk
// whose read failed lets it be read at once, and that a failure of a read
// that crossed one holds nothing off.
func TestLodestoneRetryClearedByLoadOrWrite(t *testing.T) {
	var c lodestoneCache
	pos := ChunkPos{X: 1, Z: 2}
	version := c.begin(pos)
	c.fail(pos, version)
	if !c.holding(pos) {
		t.Fatal("a failed read is not held off")
	}
	c.drop(pos)
	if c.holding(pos) {
		t.Error("a load or write left the failed read held off")
	}
	c.fail(pos, version) // began before the drop
	c.end(pos)
	if c.holding(pos) {
		t.Error("a read that crossed a load or write held its chunk off")
	}
}

// TestLodestoneAnswersSeeLoadAndWrite checks an answer read as absent is not
// used once its chunk loaded, took a lodestone and was saved, though the batch
// still holds it.
func TestLodestoneAnswersSeeLoadAndWrite(t *testing.T) {
	w := writeSavedRoot(t, savedLodestoneRoot(t, "minecraft:full", "minecraft:stone"))
	p := LodestonePos{1, 64, 1}
	got := w.ResolveLodestones([]LodestonePos{p})
	defer got.Close()
	if present, known := got.Lookup(p); !known || present {
		t.Fatalf("answer = %v, %v, want absent and known", present, known)
	}
	pos := ChunkPos{}
	w.Acquire(pos)
	w.SetBlock(p.X, p.Y, p.Z, registry.BlockLodestone.DefaultState())
	w.Save()
	w.Release(pos)
	for w.ChunkLoaded(pos) {
		runtime.Gosched()
	}
	if _, known := got.Lookup(p); known {
		t.Error("the answer outlived a load and write of its chunk")
	}
	if _, known := w.LodestoneCached(p.X, p.Y, p.Z); known {
		t.Error("the cache kept an answer across a load and write")
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
		got := w.ResolveLodestones([]LodestonePos{{50, 63, 70}})
		if present, _ := got.Lookup(LodestonePos{50, 63, 70}); !present {
			b.Fatal("the saved lodestone was not found")
		}
		got.Close()
	}
}

// TestBlockStatesAtMatchesDecode checks the direct palette read agrees with
// the section decode for a packed section.
func TestBlockStatesAtMatchesDecode(t *testing.T) {
	var names []nbt.Tag
	for _, n := range []string{"minecraft:air", "minecraft:stone", "minecraft:lodestone"} {
		names = append(names, nbt.Compound{"Name": nbt.String(n)})
	}
	data := make(nbt.LongArray, 342) // 2 bits wide is raised to 4: 16 per long
	data = data[:blocksPerSection/16]
	for i := range data {
		data[i] = int64(0x2121_1020_0121_2012) * int64(i+1)
	}
	bs := nbt.Compound{"palette": nbt.List{Elem: nbt.TypeCompound, Items: names}, "data": data}
	var s section
	s.fill = airState
	if err := decodeSectionBlocks(&s, bs); err != nil {
		t.Fatal(err)
	}
	cells := []int{0, 1, 15, 16, 255, 256, 1000, 4095}
	out := make([]uint16, len(cells))
	if err := blockStatesAt(bs, cells, out); err != nil {
		t.Fatal(err)
	}
	for i, cell := range cells {
		if want := s.blocks.Load().get(cell); out[i] != want {
			t.Errorf("cell %d = %d, want %d", cell, out[i], want)
		}
	}
}
