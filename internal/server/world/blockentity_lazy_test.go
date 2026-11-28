package world

import (
	"slices"
	"testing"
)

func TestUnloadedBlockEntitiesLeaveTheListsInOrder(t *testing.T) {
	w := newTestWorld(t, NewFlatGenerator())
	var want []*BlockEntity
	for cx := range int32(4) {
		w.Acquire(ChunkPos{X: cx})
		for i := range int32(4) {
			want = append(want, w.EnsureBlockEntity(cx*16+i, GroundLevel, 0, KindHopper))
		}
	}
	without := func(lo, hi int) []*BlockEntity { return slices.Delete(slices.Clone(want), lo, hi) }
	unload := func(cx int32) (cleared, listed int) {
		w.mu.Lock()
		defer w.mu.Unlock()
		w.removeChunkBlockEntities(w.chunks[ChunkPos{X: cx}].chunk)
		return w.removedInLists, len(w.blockEntityOrder.bes)
	}
	check := func(name string, got, want []*BlockEntity) {
		t.Helper()
		if !slices.Equal(got, want) {
			t.Fatalf("%s: %d entities, want %d in order", name, len(got), len(want))
		}
	}
	check("ticking", w.TickingBlockEntities(), want)
	// One of four chunks: below the threshold, the cleared slots linger.
	if cleared, listed := unload(1); cleared != 4 || listed != 16 {
		t.Fatalf("cleared=%d listed=%d, want 4 and 16", cleared, listed)
	}
	want = without(4, 8)
	check("kind", w.BlockEntitiesOfKind(KindHopper), want)
	// The tick's read drops them.
	check("ticking", w.TickingBlockEntities(), want)
	w.mu.RLock()
	cleared, listed := w.removedInLists, len(w.blockEntityOrder.bes)
	w.mu.RUnlock()
	if cleared != 0 || listed != 12 {
		t.Fatalf("after the tick's read cleared=%d listed=%d, want 0 and 12", cleared, listed)
	}
	if cleared, listed := unload(2); cleared != 4 || listed != 12 {
		t.Fatalf("cleared=%d listed=%d, want 4 and 12", cleared, listed)
	}
	// Over half of the list: the removal itself compacts.
	if cleared, listed := unload(3); cleared != 0 || listed != 4 {
		t.Fatalf("cleared=%d listed=%d, want 0 and 4", cleared, listed)
	}
	want = want[:4]
	check("kind", w.BlockEntitiesOfKind(KindHopper), want)
	check("ticking", w.TickingBlockEntities(), want)
}
