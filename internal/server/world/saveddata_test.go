package world

import (
	"bytes"
	"os"
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

// TestSavedDataWriteLeavesNoTempFile checks a saved data file goes in whole
// through its temp file, which the rename takes away, and reads back.
func TestSavedDataWriteLeavesNoTempFile(t *testing.T) {
	dir := t.TempDir()
	blob := nbt.ByteArray(bytes.Repeat([]byte{7}, 4<<20))
	if err := writeSavedData(dir, "data/minecraft/test.dat", nbt.Compound{"blob": blob}); err != nil {
		t.Fatalf("writeSavedData: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "data/minecraft/test.dat.tmp")); !os.IsNotExist(err) {
		t.Errorf("the temp file is still there: %v", err)
	}
	data, err := readSavedData(dir, "data/minecraft/test.dat")
	if err != nil {
		t.Fatalf("readSavedData: %v", err)
	}
	if got, _ := nbt.Get[nbt.ByteArray](data, "blob"); !bytes.Equal(got, blob) {
		t.Errorf("read back %d bytes, want the %d written", len(got), len(blob))
	}
}
