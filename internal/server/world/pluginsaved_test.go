package world

import "testing"

func TestAcquireSavedRejectsMissingAndDamagedChunks(t *testing.T) {
	w, err := NewWorld(t.TempDir(), NewFlatGenerator())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = w.Close() })
	w.SetChunkGenerationGate(func(ChunkPos) { t.Error("saved-only acquisition generated terrain") })
	pos := ChunkPos{X: 30, Z: 30}
	if w.AcquireSaved(pos) != nil || w.ChunkLoaded(pos) {
		t.Fatal("missing terrain was loaded")
	}
	rx, rz := regionCoords(pos)
	rf, err := w.region(rx, rz)
	if err != nil {
		t.Fatal(err)
	}
	err = rf.writeChunk(pos, []byte{1, 2, 3})
	w.releaseRegion(rf)
	if err != nil {
		t.Fatal(err)
	}
	if !w.HasChunk(pos) {
		t.Fatal("damaged chunk was not present in the region file")
	}
	if w.AcquireSaved(pos) != nil || w.ChunkLoaded(pos) {
		t.Fatal("damaged terrain was generated or installed")
	}
}
