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

func TestUnloadedBlockEntitiesLeaveTheListsInOrder(t *testing.T) {
	w := newTestWorld(t, NewFlatGenerator())
	var want []*BlockEntity
	for cx := range int32(4) {
		w.Acquire(ChunkPos{X: cx})
		for i := range int32(4) {
			want = append(want, w.EnsureBlockEntity(cx*16+i, GroundLevel, 0, KindHopper))
		}
	}
	check := func(want []*BlockEntity) {
		t.Helper()
		for name, got := range map[string][]*BlockEntity{
			"ticking": w.TickingBlockEntities(),
			"kind":    w.BlockEntitiesOfKind(KindHopper),
		} {
			if len(got) != len(want) {
				t.Fatalf("%s: %d entities, want %d", name, len(got), len(want))
			}
			for i := range got {
				if got[i] != want[i] {
					t.Fatalf("%s: entry %d out of order", name, i)
				}
			}
		}
	}
	check(want)
	// One of four chunks: below the compaction threshold, entries linger.
	w.mu.Lock()
	w.removeChunkBlockEntities(w.chunks[ChunkPos{X: 1}].chunk)
	lingering := w.removedInLists
	w.mu.Unlock()
	if lingering != 4 {
		t.Fatalf("removedInLists = %d, want 4", lingering)
	}
	check(append(append([]*BlockEntity{}, want[:4]...), want[8:]...))
	// Exactly half is not over half; the next chunk compacts.
	w.mu.Lock()
	w.removeChunkBlockEntities(w.chunks[ChunkPos{X: 2}].chunk)
	lingering = w.removedInLists
	w.mu.Unlock()
	if lingering != 8 {
		t.Fatalf("removedInLists = %d, want 8", lingering)
	}
	check(append(append([]*BlockEntity{}, want[:4]...), want[12:]...))
	w.mu.Lock()
	w.removeChunkBlockEntities(w.chunks[ChunkPos{X: 3}].chunk)
	n, left := w.removedInLists, len(w.blockEntityOrder)
	w.mu.Unlock()
	if n != 0 || left != 4 {
		t.Fatalf("after compaction removedInLists=%d list=%d, want 0 and 4", n, left)
	}
	check(want[:4])
}
