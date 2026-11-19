package world

import (
	"math"
	"testing"
)

func TestBlockSnapshotMatchesLoadedSections(t *testing.T) {
	t.Parallel()
	w := newTestWorld(t, NewFlatGenerator())
	for i, pos := range []ChunkPos{{-1, -1}, {-1, 0}, {0, -1}, {0, 0}} {
		c, _ := w.Acquire(pos)
		defer w.Release(pos)
		// Uniform, four-bit, five-bit and direct palettes.
		states := []int{1, 15, 31, 257}[i]
		for y := int32(48); y < 80; y++ {
			for z := range int32(16) {
				for x := range int32(16) {
					c.SetBlock(x, y, z, uint16(1+int(x+16*z+256*(y-48))%states))
				}
			}
		}
	}
	snapshot := w.SnapshotBlocks()
	defer snapshot.Release()
	for x := int32(-18); x <= 18; x++ {
		for y := int32(46); y <= 81; y++ {
			for z := int32(-18); z <= 18; z++ {
				if got, want := snapshot.BlockAt(x, y, z), w.BlockAt(x, y, z); got != want {
					t.Fatalf("snapshot at (%d,%d,%d) = %d, want %d", x, y, z, got, want)
				}
			}
		}
	}
	for _, y := range []int32{math.MinInt32, worldMinY - 1, worldMinY, worldMinY + worldHeight - 1, worldMinY + worldHeight, math.MaxInt32} {
		if got, want := snapshot.BlockAt(0, y, 0), w.BlockAt(0, y, 0); got != want {
			t.Fatalf("snapshot at y=%d = %d, want %d", y, got, want)
		}
	}
}
