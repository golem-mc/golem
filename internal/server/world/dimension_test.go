package world

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// The wire and disk forms a dimension's chunks take. In memory every
// dimension is the overworld's height; on the wire and on disk the nether is
// the 256 blocks of its own dimension type and the end is its 256, and these
// tests pin that a chunk leaves this server in the shape the client's own
// data pack will parse.

// TestNetherChunkEncodesItsOwnSections checks that a nether chunk's wire form
// is the sixteen sections of the nether's dimension type, not the
// twenty-four of the memory it was built in: y 0..255 starting at in-memory
// section four.
func TestNetherChunkEncodesItsOwnSections(t *testing.T) {
	t.Parallel()
	w := NewDimensionWorld(t.TempDir(), true, NewNetherGenerator(7))
	defer w.Close()
	pos := ChunkPos{X: 0, Z: 0}
	chunk, _ := w.Acquire(pos)
	defer w.Release(pos)

	want := chunk.encodeChunkSections(4, 16)
	if len(want) == 0 {
		t.Fatal("the nether chunk's wire sections are empty")
	}
	if got := w.encodeChunkWired(chunk); string(got) != string(want) {
		t.Errorf("a nether chunk encodes %d bytes, want the %d of its own band",
			len(got), len(want))
	}
	// The full-height memory encoding is longer: sections above and below
	// the band are not nothing.
	if full := encodeWhole(chunk); len(full) <= len(want) {
		t.Errorf("the full-height encoding is %d bytes, want more than the nether's %d",
			len(full), len(want))
	}
}

// TestOverworldChunkEncodesEverything checks that the overworld's wire band
// is the whole of memory, which is what every chunk of it has always been.
func TestOverworldChunkEncodesEverything(t *testing.T) {
	t.Parallel()
	w := newTestWorld(t, NewFlatGenerator())
	defer w.Close()
	pos := ChunkPos{X: 0, Z: 0}
	chunk, _ := w.Acquire(pos)
	defer w.Release(pos)

	if got, want := string(w.encodeChunkWired(chunk)), string(encodeWhole(chunk)); got != want {
		t.Error("an overworld chunk encodes a clipped form, want the full height")
	}
}

// readLightMasks reads the four BitSets off the front of a light payload —
// the sky mask, the block mask, and the two empty masks — in the wire form
// encodeLight and encodeLightSpan both write.
func readLightMasks(t *testing.T, payload []byte) (skyMask, blockMask, emptySkyMask, emptyBlockMask uint64) {
	t.Helper()
	r := protocol.NewReader(payload)
	read := r.ReadBitSet
	skyMask = read()
	blockMask = read()
	emptySkyMask = read()
	emptyBlockMask = read()
	if err := r.Err(); err != nil {
		t.Fatalf("read light masks: %v", err)
	}
	return skyMask, blockMask, emptySkyMask, emptyBlockMask
}

// TestNetherLightIsCutAndSkyless checks the light a nether chunk leaves with:
// only the sections of its band — sixteen of the world plus the one below and
// the one above — and an all-zero sky half, since the nether's dimension type
// sends no sky light down. The block half is whatever the emitters of the
// terrain lit, so it is checked for shape rather than for light: everything
// is either lit or empty, and nothing outside the band exists.
func TestNetherLightIsCutAndSkyless(t *testing.T) {
	t.Parallel()
	w := NewDimensionWorld(t.TempDir(), true, NewNetherGenerator(7))
	defer w.Close()
	pos := ChunkPos{X: 0, Z: 0}
	chunk, _ := w.Acquire(pos)
	defer w.Release(pos)

	skyMask, blockMask, emptySkyMask, emptyBlockMask :=
		readLightMasks(t, w.lightWired(chunk))

	// Eighteen light sections: the nether's sixteen plus one below and one
	// above, and nothing beyond them.
	for i := range 18 {
		if skyMask&(1<<i) != 0 {
			t.Errorf("light section %d is in the sky mask, want no sky light in the nether", i)
		}
		if emptySkyMask&(1<<i) == 0 {
			t.Errorf("light section %d is not in the empty sky mask, want every sky section empty", i)
		}
		if blockMask&(1<<i) != 0 && emptyBlockMask&(1<<i) != 0 {
			t.Errorf("light section %d is both lit and empty", i)
		}
		if blockMask&(1<<i) == 0 && emptyBlockMask&(1<<i) == 0 {
			t.Errorf("light section %d is neither lit nor empty, want the masks to cover it", i)
		}
	}
	if skyMask>>18 != 0 || blockMask>>18 != 0 || emptySkyMask>>18 != 0 || emptyBlockMask>>18 != 0 {
		t.Error("light sections above the nether's band are named in the masks")
	}
}

// TestEndLightCarriesSkyLight checks the end's light: eighteen sections — the
// end's sixteen plus the pair around it — and a sky half, the end's dimension
// type having has_skylight true. The open sky over the arrival platform's
// chunk reads 15 at every Y, and the sections the chunk stores leave in the
// sky mask.
func TestEndLightCarriesSkyLight(t *testing.T) {
	t.Parallel()
	w := NewDimensionWorld(t.TempDir(), false, NewEndGenerator(7))
	defer w.Close()
	pos := ChunkPos{X: 6, Z: 0} // the chunk the arrival platform straddles
	chunk, _ := w.Acquire(pos)
	defer w.Release(pos)

	skyMask, blockMask, emptySkyMask, emptyBlockMask := readLightMasks(t, w.lightWired(chunk))
	if skyMask == 0 {
		t.Error("the end's sky mask is empty, want sky light in the sections")
	}
	for i := range 18 {
		if skyMask&(1<<i) != 0 && emptySkyMask&(1<<i) != 0 {
			t.Errorf("light section %d is both lit and empty from the sky", i)
		}
		if blockMask&(1<<i) != 0 && emptyBlockMask&(1<<i) != 0 {
			t.Errorf("light section %d is both lit and empty", i)
		}
	}
	if skyMask>>18 != 0 || emptySkyMask>>18 != 0 {
		t.Error("light sections above the end's band are named in the masks")
	}
	if got := w.SkyLightAt(6*16+8, 255, 8); got != 15 {
		t.Errorf("sky light under the end's open sky = %d, want 15", got)
	}
}

// TestNetherChunkSavesItsOwnBand checks the disk form: the compound a nether
// chunk saves holds only the sections of its band, numbered from the nether's
// own floor, with a yPos to match — the shape vanilla's nether region files
// keep — in the nether's own dimension folder, and no level.dat of its own.
func TestNetherChunkSavesItsOwnBand(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	w := NewDimensionWorld(dir, true, NewNetherGenerator(7))
	defer w.Close()
	pos := ChunkPos{X: 0, Z: 0}
	obsidian, ok := registry.FindState("minecraft:obsidian", nil)
	if !ok {
		t.Fatal("the block registry has no minecraft:obsidian")
	}
	// The chunk has to be loaded for the write, and held while it saves.
	w.Acquire(pos)
	defer w.Release(pos)
	if !w.SetBlock(0, 33, 0, obsidian) {
		t.Fatal("the block did not change, so the chunk would not save")
	}
	w.Save()
	w.FlushSaves()

	// Read the saved compound straight off the region file.
	rf, err := w.region(regionCoords(pos))
	if err != nil {
		t.Fatalf("open the nether's region file: %v", err)
	}
	data, err := rf.readChunk(pos)
	if err != nil || data == nil {
		t.Fatalf("the nether chunk did not save: %v (present: %v)", err, data != nil)
	}
	_, tag, err := nbt.Decode(data)
	if err != nil {
		t.Fatalf("parse the saved chunk: %v", err)
	}
	root, ok := tag.(nbt.Compound)
	if !ok {
		t.Fatalf("the saved chunk's root is a %s, want a compound", tag.Type())
	}
	if got, ok := nbt.Get[nbt.Int](root, "yPos"); !ok || int32(got) != 0 {
		t.Errorf("yPos = %v, %v; want 0, the nether's own first section", got, ok)
	}
	sections, _ := nbt.Get[nbt.List](root, "sections")
	if len(sections.Items) != 16 {
		t.Fatalf("the saved nether chunk holds %d sections, want the nether's 16", len(sections.Items))
	}
	for i, item := range sections.Items {
		sec, ok := item.(nbt.Compound)
		if !ok {
			t.Fatalf("saved section %d is a %s, want a compound", i, item.Type())
		}
		y, _ := nbt.Get[nbt.Byte](sec, "Y")
		if int32(y) != int32(i) {
			t.Errorf("saved section %d says it is Y=%d, want %d", i, y, i)
		}
	}

	// The nether's chunks go under its own dimension folder, and only the
	// overworld's world writes a level.dat.
	if _, err := os.Stat(filepath.Join(dir, "dimensions", "minecraft", "the_nether", "region", "r.0.0.mca")); err != nil {
		t.Errorf("the nether's region file is not in its dimension folder: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "level.dat")); err == nil {
		t.Error("the nether wrote a level.dat, want the vanilla layout without one")
	}
}

// TestDimensionVoidStartsAtItsOwnFloor checks where falling out of the world
// begins in each dimension: 64 below the dimension's own floor, the
// overworld's being where the old fixed figure of -128 came from.
func TestDimensionVoidStartsAtItsOwnFloor(t *testing.T) {
	t.Parallel()
	if got := newTestWorld(t, NewFlatGenerator()).VoidY(); got != -128 {
		t.Errorf("overworld void Y = %d, want -128", got)
	}
	nether := NewDimensionWorld(t.TempDir(), true, NewNetherGenerator(7))
	defer nether.Close()
	if got := nether.VoidY(); got != -64 {
		t.Errorf("nether void Y = %d, want -64", got)
	}
	end := NewDimensionWorld(t.TempDir(), false, NewEndGenerator(7))
	defer end.Close()
	if got := end.VoidY(); got != -64 {
		t.Errorf("end void Y = %d, want -64", got)
	}
}

// TestHasChunkReportsSavedChunksOnly checks the portal search's shortcut: a
// chunk nobody has visited is nothing to load, and a generated one reads back
// as there once its first unload has saved it, as does a chunk someone
// changed.
func TestHasChunkReportsSavedChunksOnly(t *testing.T) {
	t.Parallel()
	w := NewDimensionWorld(t.TempDir(), true, NewNetherGenerator(7))
	defer w.Close()
	pos := ChunkPos{X: 3, Z: -2}
	if w.HasChunk(pos) {
		t.Error("a chunk that was never saved reports as there")
	}

	chunk, _ := w.Acquire(pos)
	w.Release(pos)
	if chunk == nil {
		t.Fatal("the chunk did not generate")
	}
	w.FlushSaves()
	if !w.HasChunk(pos) {
		t.Error("a generated and released chunk does not report as there")
	}

	w.Acquire(pos)
	if !w.SetBlock(3*16+2, 40, -2*16+2, stoneState) {
		t.Fatal("the block did not change, so the chunk would not save")
	}
	w.Release(pos)
	w.Save()
	w.FlushSaves()
	if !w.HasChunk(pos) {
		t.Error("a saved chunk does not report as there")
	}
}
