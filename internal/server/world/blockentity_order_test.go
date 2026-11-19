package world

import (
	"slices"
	"testing"
)

// entityPositions reduces an entity list to the positions it names, so tests
// can compare orders across a save and load, where the entities are new
// pointers.
func entityPositions(entities []*BlockEntity) [][3]int32 {
	out := make([][3]int32, len(entities))
	for i, b := range entities {
		out[i] = [3]int32{b.X, b.Y, b.Z}
	}
	return out
}

// TestABlockEntityOrderSurvivesSaveLoadAndSave checks the round trip: the
// order a chunk saves its block entities in is the order a reload re-adds
// them and the order the next save writes, so two saves of one chunk agree on
// the bytes and a chain across a border keeps its order.
func TestABlockEntityOrderSurvivesSaveLoadAndSave(t *testing.T) {
	t.Parallel()
	w := newTestWorld(t, NewFlatGenerator())
	// Four chunks, so a world that rebuilds the order by ranging the chunk
	// map cannot happen to agree twice.
	chunks := []ChunkPos{{X: 0, Z: 0}, {X: 1, Z: 0}, {X: 0, Z: 1}, {X: 1, Z: 1}}
	for _, pos := range chunks {
		w.Acquire(pos)
	}
	add := func(x, z int32, kind BlockEntityKind) {
		if w.EnsureBlockEntity(x, GroundLevel, z, kind) == nil {
			t.Fatalf("could not place a %s at (%d,%d)", kind, x, z)
		}
	}
	// Added grouped by chunk, in the order the chunks were acquired.
	add(9, 3, KindChest)
	add(2, 11, KindFurnace)
	add(18, 5, KindBarrel)
	add(22, 1, KindChest)
	add(4, 18, KindChest)
	add(12, 20, KindFurnace)
	add(20, 18, KindBarrel)
	add(28, 22, KindChest)
	want := entityPositions(w.TickingBlockEntities())

	saveLoad := func() {
		for _, pos := range chunks {
			w.Release(pos)
		}
		w.FlushSaves()
		for _, pos := range chunks {
			w.Acquire(pos)
		}
	}
	saveLoad()
	if got := entityPositions(w.TickingBlockEntities()); !slices.Equal(got, want) {
		t.Fatalf("order after a save and load = %v, want %v", got, want)
	}
	saveLoad()
	if got := entityPositions(w.TickingBlockEntities()); !slices.Equal(got, want) {
		t.Fatalf("order after a second save and load = %v, want %v", got, want)
	}
	for _, pos := range chunks {
		w.Release(pos)
	}
}
