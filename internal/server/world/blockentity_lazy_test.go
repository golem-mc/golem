package world

import "testing"

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
