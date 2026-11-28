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

// BenchmarkAppendTickingBlockEntities reads 8000 hoppers' tick list. In
// "afterbreak" each op first breaks and replaces one hopper, so the read comes
// after a removal; the "read" cases then read each entry's Kind, as
// tickBlockEntities does right after its copy.
func BenchmarkAppendTickingBlockEntities(b *testing.B) {
	w := lazyFixture(b, 8000)
	w.Acquire(ChunkPos{})
	w.EnsureBlockEntity(0, GroundLevel, 0, KindHopper)
	buf := w.AppendTickingBlockEntities(nil)
	for _, brk := range []bool{false, true} {
		for _, read := range []bool{false, true} {
			name := map[bool]string{false: "live", true: "afterbreak"}[brk]
			if read {
				name += "+read"
			}
			b.Run(name, func(b *testing.B) {
				hoppers := 0
				for range b.N {
					if brk {
						w.RemoveBlockEntity(0, GroundLevel, 0)
						w.EnsureBlockEntity(0, GroundLevel, 0, KindHopper)
					}
					buf = w.AppendTickingBlockEntities(buf[:0])
					if read {
						for _, be := range buf {
							if be.Kind == KindHopper {
								hoppers++
							}
						}
					}
				}
				if len(buf) != 8001 {
					b.Fatalf("got %d ticking, want 8001", len(buf))
				}
			})
		}
	}
}
