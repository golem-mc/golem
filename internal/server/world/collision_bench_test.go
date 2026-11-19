package world

import "testing"

// benchCollisionWorld is a flat world with a column of stone, a fence and
// a slab beside the cells the benchmarks' box walks.
func benchCollisionWorld(b *testing.B) *World {
	b.Helper()
	w, _ := NewWorld(b.TempDir(), NewFlatGenerator())
	b.Cleanup(func() { _ = w.Close() })
	for x := int32(-2); x < 2; x++ {
		for z := int32(-2); z < 2; z++ {
			w.Acquire(ChunkPos{X: x, Z: z})
		}
	}
	return w
}

// BenchmarkBoxCollides is the pose test's box walk: a standing player's box
// in the open, over flat ground.
func BenchmarkBoxCollides(b *testing.B) {
	w := benchCollisionWorld(b)
	y := float64(GroundLevel)
	b.ReportAllocs()
	for i := 0; b.Loop(); i++ {
		x := float64(i&15) + 0.5
		w.BoxCollides(x-0.3, y+0.001, 8.2, x+0.3, y+1.8, 8.8)
	}
}

// BenchmarkCollidesAnew is the move check made for every movement packet.
func BenchmarkCollidesAnew(b *testing.B) {
	w := benchCollisionWorld(b)
	y := float64(GroundLevel)
	b.ReportAllocs()
	for i := 0; b.Loop(); i++ {
		x := float64(i&15) + 0.5
		w.CollidesAnew([6]float64{x - 0.3, y, 8.2, x + 0.3, y + 1.8, 8.8}, [6]float64{x - 0.2, y, 8.2, x + 0.4, y + 1.8, 8.8})
	}
}
