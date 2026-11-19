package world

import (
	"math"
	"testing"
)

// BenchmarkBlocksIn reads packed sections across a chunk corner and y=64.
func BenchmarkBlocksIn(b *testing.B) {
	w := newTestWorld(b, NewFlatGenerator())
	for cx := range int32(2) {
		for cz := range int32(2) {
			pos := ChunkPos{cx, cz}
			c, _ := w.Acquire(pos)
			defer w.Release(pos)
			for y := int32(48); y < 80; y++ {
				for z := range int32(16) {
					for x := range int32(16) {
						c.SetBlock(x, y, z, uint16(1+(x+3*z+5*y)%15))
					}
				}
			}
			for _, sy := range []int{7, 8} {
				if words := c.sections[sy].blocks.Load(); words == nil || words.bits != 4 {
					b.Fatal("benchmark requires four-bit sections")
				}
			}
		}
	}
	for _, tc := range []struct {
		name   string
		lo, hi [3]int32
	}{
		{"3x5x3", [3]int32{14, 62, 14}, [3]int32{16, 66, 16}},
		{"16x16x16", [3]int32{8, 56, 8}, [3]int32{23, 71, 23}},
	} {
		b.Run(tc.name, func(b *testing.B) {
			buf := w.BlocksIn(tc.lo[0], tc.lo[1], tc.lo[2], tc.hi[0], tc.hi[1], tc.hi[2], nil)
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				buf = w.BlocksIn(tc.lo[0], tc.lo[1], tc.lo[2], tc.hi[0], tc.hi[1], tc.hi[2], buf)
			}
		})
	}
}

// TestBlocksInReadsWhatBlockAtReads holds the box read equal to BlockAt cell
// by cell over a box spanning a chunk border, an unloaded chunk and the
// world's floor.
func TestBlocksInReadsWhatBlockAtReads(t *testing.T) {
	t.Parallel()
	w := newTestWorld(t, NewOverworldGenerator(5))
	w.Acquire(ChunkPos{0, 0})
	w.Acquire(ChunkPos{-1, 0})
	defer w.Release(ChunkPos{0, 0})
	defer w.Release(ChunkPos{-1, 0})
	w.SetBlock(0, -64, 3, torchState)
	w.SetBlock(-1, -63, 15, stoneState)
	x0, y0, z0, x1, y1, z1 := int32(-3), int32(worldMinY-2), int32(12), int32(2), int32(worldMinY+3), int32(18)
	got := w.BlocksIn(x0, y0, z0, x1, y1, z1, make([]uint16, 3))
	i := 0
	for x := x0; x <= x1; x++ {
		for y := y0; y <= y1; y++ {
			for z := z0; z <= z1; z, i = z+1, i+1 {
				if want := w.BlockAt(x, y, z); got[i] != want {
					t.Fatalf("BlocksIn at (%d,%d,%d) = %d, BlockAt = %d", x, y, z, got[i], want)
				}
			}
		}
	}
	if i != len(got) {
		t.Fatalf("BlocksIn read %d cells, want %d", len(got), i)
	}
}

func TestBlocksInMatchesBlockAtAcrossSections(t *testing.T) {
	t.Parallel()
	w := newTestWorld(t, NewFlatGenerator())
	for i, pos := range []ChunkPos{{-1, -1}, {-1, 0}, {0, -1}, {0, 0}} {
		c, _ := w.Acquire(pos)
		defer w.Release(pos)
		// Uniform, four-bit, five-bit and direct sections, with different
		// patterns on either side of y=64.
		states := []int{1, 15, 31, 257}[i]
		for y := int32(48); y < 80; y++ {
			for z := range int32(16) {
				for x := range int32(16) {
					c.SetBlock(x, y, z, uint16(1+(int(x+16*z+256*(y-48)))%states))
				}
			}
		}
	}
	for _, tc := range []struct {
		name string
		box  [6]int32
	}{
		{"chunk-and-section-corners", [6]int32{-18, 46, -18, 18, 81, 18}},
		{"floor", [6]int32{-18, worldMinY - 2, -2, 2, worldMinY + 2, 2}},
		{"ceiling", [6]int32{-2, worldMinY + worldHeight - 2, -2, 2, worldMinY + worldHeight + 2, 2}},
		{"below", [6]int32{-2, worldMinY - 18, -2, 2, worldMinY - 1, 2}},
		{"above", [6]int32{-2, worldMinY + worldHeight, -2, 2, worldMinY + worldHeight + 18, 2}},
		{"single-cell", [6]int32{0, 64, 0, 0, 64, 0}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assertBlocksInMatchesBlockAt(t, w, tc.box)
		})
	}
}

func TestBlocksInCoordinateLimitsAndEmptyBoxes(t *testing.T) {
	t.Parallel()
	w := newTestWorld(t, NewFlatGenerator())
	for _, pos := range []ChunkPos{{math.MaxInt32 >> 4, math.MaxInt32 >> 4}, {math.MinInt32 >> 4, math.MinInt32 >> 4}} {
		w.Acquire(pos)
		defer w.Release(pos)
	}
	for _, box := range [][6]int32{
		{math.MaxInt32 - 2, 62, math.MaxInt32 - 2, math.MaxInt32, 66, math.MaxInt32},
		{math.MinInt32, 62, math.MinInt32, math.MinInt32 + 2, 66, math.MinInt32 + 2},
		{0, math.MaxInt32 - 2, 0, 2, math.MaxInt32, 2},
		{0, math.MinInt32, 0, 2, math.MinInt32 + 2, 2},
	} {
		assertBlocksInMatchesBlockAt(t, w, box)
	}
	for _, box := range [][6]int32{{1, 0, 0, 0, 1, 1}, {0, 1, 0, 1, 0, 1}, {0, 0, 1, 1, 1, 0}} {
		if got := w.BlocksIn(box[0], box[1], box[2], box[3], box[4], box[5], []uint16{stoneState}); len(got) != 0 {
			t.Fatalf("BlocksIn%v returned %v, want empty", box, got)
		}
	}
}

func assertBlocksInMatchesBlockAt(t *testing.T, w *World, box [6]int32) {
	t.Helper()
	nx, ny, nz := int(box[3]-box[0]+1), int(box[4]-box[1]+1), int(box[5]-box[2]+1)
	buf := make([]uint16, nx*ny*nz+1)
	for i := range buf {
		buf[i] = math.MaxUint16
	}
	got := w.BlocksIn(box[0], box[1], box[2], box[3], box[4], box[5], buf[:1])
	if len(got) != len(buf)-1 || &got[0] != &buf[0] || buf[len(buf)-1] != math.MaxUint16 {
		t.Fatal("BlocksIn did not reuse the buffer within the box's length")
	}
	for dx := range nx {
		for dy := range ny {
			for dz := range nz {
				x, y, z := box[0]+int32(dx), box[1]+int32(dy), box[2]+int32(dz)
				i := (dx*ny+dy)*nz + dz
				if want := w.BlockAt(x, y, z); got[i] != want {
					t.Fatalf("BlocksIn at (%d,%d,%d) = %d, BlockAt = %d", x, y, z, got[i], want)
				}
			}
		}
	}
}

func TestAirInIncludesTheCellAtAMaxEdgeOnABlockBoundary(t *testing.T) {
	t.Parallel()
	w := newTestWorld(t, NewOverworldGenerator(5))
	w.Acquire(ChunkPos{0, 0})
	defer w.Release(ChunkPos{0, 0})
	w.SetBlock(2, 100, 2, stoneState)
	// The box tops out exactly at y=100, the floor of the stone's cell.
	if w.AirIn([6]float64{2.2, 98.2, 2.2, 2.8, 100, 2.8}) {
		t.Fatal("AirIn skipped the cell at the top edge")
	}
	if !w.AirIn([6]float64{2.2, 98.2, 2.2, 2.8, 99.9, 2.8}) {
		t.Fatal("AirIn found a block below the top edge")
	}
}
