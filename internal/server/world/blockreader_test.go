package world

import "testing"

func TestBlockReaderMatchesBlockAt(t *testing.T) {
	w, err := NewWorld(t.TempDir(), NewFlatGenerator())
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	t.Cleanup(func() { _ = w.Close() })
	w.Acquire(ChunkPos{X: 0, Z: 0})
	w.Acquire(ChunkPos{X: 1, Z: 0})
	w.SetBlock(15, GroundLevel, 3, 1)
	w.SetBlock(16, GroundLevel, 3, 2)
	cells := [][3]int32{{15, GroundLevel, 3}, {16, GroundLevel, 3}, {15, GroundLevel, 3}, {-1, GroundLevel, 3}, {0, GroundLevel - 1, 0}, {-1, GroundLevel, 3}}
	want := make([]uint16, len(cells))
	for i, c := range cells {
		want[i] = w.BlockAt(c[0], c[1], c[2])
	}
	r := w.readBlocks()
	defer r.done()
	// Alternating chunks and an unloaded one exercise the last-chunk cache.
	for i, c := range cells {
		if got := r.at(c[0], c[1], c[2]); got != want[i] {
			t.Fatalf("at%v = %d, want %d", c, got, want[i])
		}
	}
}
