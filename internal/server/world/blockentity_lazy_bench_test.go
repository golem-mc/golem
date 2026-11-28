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

// BenchmarkUnloadChunkBlockEntities times one chunk's unload step among n
// hoppers. Each op puts a fresh chest in a held chunk rather than reloading
// it: a reload saves the chunk, which would swamp the step. The timer runs
// throughout, since StopTimer per op makes b.N, and the run, balloon.
func BenchmarkUnloadChunkBlockEntities(b *testing.B) {
	for _, n := range []int{0, 8000, 32000} {
		b.Run(fmt.Sprint(n), func(b *testing.B) {
			w := lazyFixture(b, n)
			c, _ := w.Acquire(ChunkPos{X: -1, Z: -1})
			b.ResetTimer()
			for range b.N {
				be := newBlockEntity(KindChest, -16, GroundLevel, -16)
				w.mu.Lock()
				c.setBlockEntity(be)
				w.appendBlockEntityOrder(be)
				w.removeChunkBlockEntities(c)
				w.mu.Unlock()
			}
		})
	}
}

// BenchmarkBreakBlockEntityAmongMany times placing and breaking one hopper
// among n others; the timer runs throughout, as above.
func BenchmarkBreakBlockEntityAmongMany(b *testing.B) {
	for _, n := range []int{0, 8000, 32000} {
		b.Run(fmt.Sprint(n), func(b *testing.B) {
			w := lazyFixture(b, n)
			w.Acquire(ChunkPos{})
			b.ResetTimer()
			for range b.N {
				w.EnsureBlockEntity(0, GroundLevel, 0, KindHopper)
				w.RemoveBlockEntity(0, GroundLevel, 0)
			}
		})
	}
}

// BenchmarkAppendTickingBlockEntities reads 8000 live hoppers' tick list; in
// "halfremoved" every other hopper of 16000 was broken, the most removed
// entries the list keeps before compactBlockEntityLists drops them.
func BenchmarkAppendTickingBlockEntities(b *testing.B) {
	for _, removed := range []bool{false, true} {
		name := map[bool]string{false: "live", true: "halfremoved"}[removed]
		b.Run(name, func(b *testing.B) {
			n := 8000
			if removed {
				n *= 2
			}
			w := lazyFixture(b, n)
			if removed {
				for i := 1; i < n; i += 2 {
					cx := int32(1 + i/256)
					w.RemoveBlockEntity(cx*16+int32(i%16), GroundLevel, int32(i/16%16))
				}
			}
			var buf []*BlockEntity
			if buf = w.AppendTickingBlockEntities(buf); len(buf) != 8000 {
				b.Fatalf("got %d ticking, want 8000", len(buf))
			}
			b.ResetTimer()
			for range b.N {
				buf = w.AppendTickingBlockEntities(buf[:0])
			}
		})
	}
}
