package world

import (
	"fmt"
	"testing"
)

// lazyFixture holds n hoppers loaded in chunks z=1..; chunk (0,0) is left for
// the caller.
func lazyFixture(tb testing.TB, n int) *World {
	tb.Helper()
	w := newTestWorld(tb, NewFlatGenerator())
	for i := range n {
		cx := int32(1 + i/256)
		x, z := cx*16+int32(i%16), int32(i/16%16)
		if _, _ = w.Acquire(ChunkPos{X: cx, Z: 0}); w.EnsureBlockEntity(x, GroundLevel, z, KindHopper) == nil {
			tb.Fatalf("hopper %d not created", i)
		}
	}
	return w
}

func BenchmarkUnloadChunkBlockEntities(b *testing.B) {
	for _, n := range []int{0, 8000, 32000} {
		b.Run(fmt.Sprint(n), func(b *testing.B) {
			w := lazyFixture(b, n)
			pos := ChunkPos{X: -1, Z: -1}
			b.ResetTimer()
			for range b.N {
				b.StopTimer()
				c, _ := w.Acquire(pos)
				w.EnsureBlockEntity(-16, GroundLevel, -16, KindChest)
				b.StartTimer()
				w.mu.Lock()
				w.removeChunkBlockEntities(c)
				w.mu.Unlock()
				b.StopTimer()
				w.Release(pos)
			}
		})
	}
}

func BenchmarkBreakBlockEntityAmongMany(b *testing.B) {
	for _, n := range []int{0, 8000, 32000} {
		b.Run(fmt.Sprint(n), func(b *testing.B) {
			w := lazyFixture(b, n)
			w.Acquire(ChunkPos{})
			b.ResetTimer()
			for range b.N {
				b.StopTimer()
				w.EnsureBlockEntity(0, GroundLevel, 0, KindHopper)
				b.StartTimer()
				w.RemoveBlockEntity(0, GroundLevel, 0)
			}
		})
	}
}
