package world

import "testing"

func TestChunkSetVersionChangesWithLoadAndUnload(t *testing.T) {
	t.Parallel()
	w := newTestWorld(t, NewFlatGenerator())
	pos := ChunkPos{}
	v0 := w.ChunkSetVersion()
	w.Acquire(pos)
	v1 := w.ChunkSetVersion()
	if v1 == v0 {
		t.Fatal("loading a chunk left the version unchanged")
	}
	w.Acquire(pos)
	if w.ChunkSetVersion() != v1 {
		t.Fatal("a second hold changed the version")
	}
	w.Release(pos)
	w.Release(pos)
	w.FlushSaves()
	if w.ChunkSetVersion() == v1 {
		t.Fatal("unloading a chunk left the version unchanged")
	}
}
