package world

import "testing"

// TestOverworldColumnHeightIsTheChunksHeight pins a single sampled column to
// the chunk-wide height it replaces on a cache miss.
func TestOverworldColumnHeightIsTheChunksHeight(t *testing.T) {
	t.Parallel()
	g := NewOverworldGenerator(8675309)
	for _, c := range [][2]int32{{3, 31}, {-12, 30}} {
		heights := g.chunkHeights(c[0], c[1])
		fresh := NewOverworldGenerator(8675309)
		for x := int32(0); x < 16; x += 5 {
			for z := int32(0); z < 16; z += 5 {
				if got := fresh.columnHeight(c[0]*16+x, c[1]*16+z); got != heights[x][z] {
					t.Fatalf("chunk %v column %d,%d: got %d, want %d", c, x, z, got, heights[x][z])
				}
			}
		}
	}
}
