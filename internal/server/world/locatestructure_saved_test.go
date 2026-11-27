package world

import (
	"testing"

	"github.com/golem-mc/golem/nbt"
)

// TestLocateFollowsSavedStarts checks that a saved chunk's answer replaces the
// generator's: an absent start skips the cell, a present one is found where
// the generator has none.
func TestLocateFollowsSavedStarts(t *testing.T) {
	t.Parallel()
	const id = "minecraft:mineshaft"
	ids := []string{id}
	g := structureTestGenerator(id, 50)
	bx, bz, _, ok := g.locateStructure(ids, 0, 64, 0, 20, nil, nil)
	if !ok {
		t.Fatal("generator finds nothing")
	}

	var asked [][2]int32
	_, _, _, ok = g.locateStructure(ids, 0, 64, 0, 20, nil, func(_ string, cx, cz int32) startState {
		asked = append(asked, [2]int32{cx, cz})
		return startAbsent
	})
	if ok || len(asked) < 3 {
		t.Fatalf("all cells saved without a start: found %v, asked %v", ok, asked)
	}

	// A saved chunk with no start hides the generator's hit.
	hidden := [2]int32{bx >> 4, bz >> 4}
	x, z, _, ok := g.locateStructure(ids, 0, 64, 0, 20, nil, func(_ string, cx, cz int32) startState {
		if [2]int32{cx, cz} == hidden {
			return startAbsent
		}
		return startUnknown
	})
	if ok && x == bx && z == bz {
		t.Fatalf("found the cell %v saved with no start", hidden)
	}

	// A saved start is found though the generator has none.
	want := asked[len(asked)-1]
	x, z, got, ok := g.locateStructure(ids, 0, 64, 0, 20, nil, func(_ string, cx, cz int32) startState {
		if [2]int32{cx, cz} == want {
			return startPresent
		}
		return startAbsent
	})
	if !ok || got != id || x>>4 != want[0] || z>>4 != want[1] {
		t.Fatalf("got (%d, %d) %q %v, want the saved chunk %v", x, z, got, ok, want)
	}
}

// TestSavedStartReadsRegionFile checks the answer for a loaded chunk, a chunk
// only on disk and a chunk never saved.
func TestSavedStartReadsRegionFile(t *testing.T) {
	dir := t.TempDir()
	const id = "minecraft:village_plains"
	open := func() *World {
		w, err := NewWorld(dir, NewFlatGenerator())
		if err != nil {
			t.Fatal(err)
		}
		return w
	}
	w := open()
	c, _ := w.Acquire(ChunkPos{3, 4})
	w.mu.Lock()
	c.setStructureStart(id, structureStartNBT(id, 3, 4, []nbt.Tag{nbt.Compound{}}))
	w.mu.Unlock()
	w.chunks[ChunkPos{3, 4}].markDirty()
	if got := w.savedStart(id, 3, 4); got != startPresent {
		t.Fatalf("loaded chunk: %v", got)
	}
	if got := w.savedStart("minecraft:mansion", 3, 4); got != startAbsent {
		t.Fatalf("loaded chunk, other structure: %v", got)
	}
	w.Release(ChunkPos{3, 4})
	w.Acquire(ChunkPos{5, 6})
	w.mu.Lock()
	delete(w.chunks[ChunkPos{5, 6}].chunk.savedRoot(), "structures")
	w.mu.Unlock()
	w.chunks[ChunkPos{5, 6}].markDirty()
	w.Release(ChunkPos{5, 6})
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}

	w = open()
	t.Cleanup(func() { _ = w.Close() })
	if got := w.savedStart(id, 3, 4); got != startPresent {
		t.Fatalf("saved chunk: %v", got)
	}
	if got := w.savedStart("minecraft:mansion", 3, 4); got != startAbsent {
		t.Fatalf("saved chunk, other structure: %v", got)
	}
	if got := w.savedStart(id, 5, 6); got != startUnknown {
		t.Fatalf("saved chunk without structures.starts: %v", got)
	}
	if got := w.savedStart(id, 40, 40); got != startUnknown {
		t.Fatalf("never saved chunk: %v", got)
	}
}
