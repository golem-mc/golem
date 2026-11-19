package world

import (
	"sync"
	"sync/atomic"
	"testing"
)

// surfaceBenchWorld is a generated overworld with the 3x3 chunks around the
// origin loaded, ground with hills and caves for the surface scan to walk.
func surfaceBenchWorld(b *testing.B) *World {
	dir := b.TempDir()
	w, _ := NewWorld(dir, NewGenerator(dir, "minecraft:normal", "5", "{}"))
	b.Cleanup(func() { _ = w.Close() })
	for x := int32(-1); x <= 1; x++ {
		for z := int32(-1); z <= 1; z++ {
			w.Acquire(ChunkPos{X: x, Z: z})
		}
	}
	return w
}

// forgetSurface drops the chunk's cached surface heights, so the next
// SurfaceHeight of it is a chunk's first ask again.
func forgetSurface(w *World, pos ChunkPos) {
	w.mu.Lock()
	w.chunks[pos].surface.Store(nil)
	w.mu.Unlock()
}

// BenchmarkSurfaceHeightScan is the cost of a chunk's first SurfaceHeight,
// the scan of all of its columns that the natural spawn pass sets off for
// every chunk it reaches.
func BenchmarkSurfaceHeightScan(b *testing.B) {
	w := surfaceBenchWorld(b)
	b.ResetTimer()
	for i := range b.N {
		pos := ChunkPos{X: int32(i%3) - 1, Z: int32(i/3%3) - 1}
		forgetSurface(w, pos)
		w.SurfaceHeight(pos.X*16+8, pos.Z*16+8)
	}
}

// BenchmarkBlockAtDuringSurfaceScans is the cost of a block read while first
// asks of SurfaceHeight run back to back beside it, the tick's reads against
// the spawn pass: a scan under the world's write lock made each read wait out
// the rest of it.
func BenchmarkBlockAtDuringSurfaceScans(b *testing.B) {
	w := surfaceBenchWorld(b)
	var stop atomic.Bool
	var wg sync.WaitGroup
	wg.Go(func() {
		for i := 0; !stop.Load(); i++ {
			pos := ChunkPos{X: int32(i%3) - 1, Z: int32(i/3%3) - 1}
			forgetSurface(w, pos)
			w.SurfaceHeight(pos.X*16+8, pos.Z*16+8)
		}
	})
	b.Cleanup(func() {
		stop.Store(true)
		wg.Wait()
	})

	b.ResetTimer()
	for i := range b.N {
		w.BlockAt(int32(i&31)-16, 60, 0)
	}
}

// BenchmarkSurfaceHeightParallel is the cost of a cached column's
// SurfaceHeight asked from several goroutines while block reads run beside
// them, the spawn pass's per-chunk lookups against the generation workers.
func BenchmarkSurfaceHeightParallel(b *testing.B) {
	w := surfaceBenchWorld(b)
	for x := int32(-1); x <= 1; x++ {
		for z := int32(-1); z <= 1; z++ {
			w.SurfaceHeight(x*16, z*16)
		}
	}
	var stop atomic.Bool
	var wg sync.WaitGroup
	wg.Go(func() {
		for i := 0; !stop.Load(); i++ {
			w.BlockAt(int32(i&31)-16, 60, 0)
		}
	})
	b.Cleanup(func() {
		stop.Store(true)
		wg.Wait()
	})

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for i := 0; pb.Next(); i++ {
			w.SurfaceHeight(int32(i%48)-16, int32(i/48%48)-16)
		}
	})
}
