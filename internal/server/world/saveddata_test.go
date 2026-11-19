package world

import (
	"path/filepath"
	"testing"

	"github.com/golem-mc/golem/nbt"
)

// TestManyForcedChunksReadBack checks a ticket file holding far more forced
// chunks than a few kilobytes describe is read back on the next start, every
// chunk in it.
func TestManyForcedChunksReadBack(t *testing.T) {
	const n = 2000
	dir := t.TempDir()
	w := NewDimensionWorld(dir, true, NewNetherGenerator(1))
	// Set straight into the ticket set: forcing them through SetChunkForced
	// would generate every footprint, which this test is not about.
	w.mu.Lock()
	for i := range n {
		w.forced.set[ChunkPos{X: int32(i % 50), Z: int32(i / 50)}] = struct{}{}
	}
	w.forced.dirty = true
	w.mu.Unlock()
	w.Save()
	w.Close()

	storage := filepath.Join(dir, "dimensions", "minecraft", "the_nether")
	data, err := readSavedData(storage, chunkTicketsFile)
	if err != nil {
		t.Fatalf("the ticket file: %v", err)
	}
	encoded, err := nbt.Encode("", data)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if len(encoded) <= 1<<16 {
		t.Fatalf("the ticket file is %d bytes, want one past 64 KiB for this test to mean anything", len(encoded))
	}

	again := NewDimensionWorld(dir, true, NewNetherGenerator(1))
	defer again.Close()
	if got := len(again.ForcedChunks()); got != n {
		t.Errorf("reopened with %d forced chunks, want %d", got, n)
	}
}
