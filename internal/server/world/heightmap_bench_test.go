package world

import "testing"

// BenchmarkEncodeHeightmaps is the three client heightmaps of one flat chunk,
// built each time a chunk is generated, loaded or changed.
func BenchmarkEncodeHeightmaps(b *testing.B) {
	w, _ := NewWorld(b.TempDir(), NewFlatGenerator())
	b.Cleanup(func() { _ = w.Close() })
	loaded, _ := w.Acquire(ChunkPos{})
	b.ReportAllocs()
	for range b.N {
		w.encodeHeightmaps(loaded)
	}
}
