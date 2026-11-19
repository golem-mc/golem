package world

import (
	"path/filepath"
	"testing"
)

// TestForcedChunksAreKeptPerDimension checks that the nether keeps its forced
// chunks in its own ticket file, in vanilla's order, and holds them loaded
// after a reopen.
func TestForcedChunksAreKeptPerDimension(t *testing.T) {
	dir := t.TempDir()
	nether := NewDimensionWorld(dir, true, NewNetherGenerator(1))
	for _, pos := range []ChunkPos{{X: -1, Z: 2}, {X: 3, Z: 2}, {X: 0, Z: -5}} {
		if !nether.SetChunkForced(pos, true) {
			t.Fatalf("forcing %v changed nothing", pos)
		}
	}
	if nether.SetChunkForced(ChunkPos{X: 3, Z: 2}, true) {
		t.Error("forcing a forced chunk again reported a change")
	}
	nether.Save()
	nether.Close()

	if _, err := readSavedData(filepath.Join(dir, "dimensions", "minecraft", "the_nether"), chunkTicketsFile); err != nil {
		t.Fatalf("the nether's ticket file: %v", err)
	}
	if _, err := readSavedData(filepath.Join(dir, "dimensions", "minecraft", "overworld"), chunkTicketsFile); err == nil {
		t.Error("the overworld got a ticket file")
	}

	again := NewDimensionWorld(dir, true, NewNetherGenerator(1))
	defer again.Close()
	again.HoldForcedChunks()
	got := again.ForcedChunks()
	want := []ChunkPos{{X: 0, Z: -5}, {X: 3, Z: 2}, {X: -1, Z: 2}}
	if len(got) != len(want) {
		t.Fatalf("forced = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("forced = %v, want %v", got, want)
		}
		if !again.ChunkLoaded(want[i]) {
			t.Errorf("forced chunk %v is not loaded", want[i])
		}
	}
}

// TestHoldForcedChunksCountsTheFootprints checks the count the startup log
// prints: 25 chunks around a lone forced chunk, and the union where two
// footprints overlap.
func TestHoldForcedChunksCountsTheFootprints(t *testing.T) {
	w := NewDimensionWorld(t.TempDir(), true, NewNetherGenerator(1))
	defer w.Close()
	w.SetChunkForced(ChunkPos{X: 0, Z: 0}, true)
	if got := w.HoldForcedChunks(); got != 25 {
		t.Errorf("one forced chunk held %d, want 25", got)
	}
	w.SetChunkForced(ChunkPos{X: 1, Z: 0}, true)
	if got := w.HoldForcedChunks(); got != 30 {
		t.Errorf("two adjacent forced chunks held %d, want 30", got)
	}
}
