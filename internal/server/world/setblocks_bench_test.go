package world

import "testing"

// BenchmarkSetBlocks measures a crater-sized batch (500 changes across 2
// chunks) both when every write lands and when every cell already matches.
func BenchmarkSetBlocks(b *testing.B) {
	w := newTestWorld(b, NewFlatGenerator())
	c0, c1 := ChunkPos{X: 0, Z: 0}, ChunkPos{X: 1, Z: 0}
	w.Acquire(c0)
	w.Acquire(c1)
	defer w.Release(c0)
	defer w.Release(c1)

	// 500 cells split across two chunks in chunk-contiguous order, matching
	// explosion crater writes.
	const n = 500
	toStone := make([]BlockChange, 0, n)
	toAir := make([]BlockChange, 0, n)
	for i := range n {
		cx := int32(0)
		if i >= n/2 {
			cx = 16
		}
		j := i % (n / 2)
		x := cx + int32(j&15)
		z := int32((j >> 4) & 15)
		y := int32(80 + (j >> 8))
		toStone = append(toStone, BlockChange{X: x, Y: y, Z: z, State: stoneState})
		toAir = append(toAir, BlockChange{X: x, Y: y, Z: z, State: airState})
	}

	// Warm up both chunk sections so the loop measures SetBlocks itself.
	w.SetBlocks(toStone)
	w.SetBlocks(toAir)

	b.Run("Apply", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; b.Loop(); i++ {
			if i&1 == 0 {
				w.SetBlocks(toStone)
			} else {
				w.SetBlocks(toAir)
			}
		}
	})

	w.SetBlocks(toAir)
	b.Run("NoOp", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			w.SetBlocks(toAir)
		}
	})
}
