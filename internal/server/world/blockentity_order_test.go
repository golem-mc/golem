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

// TestBlockEntitiesKeepAddOrderAcrossChunks checks that the world's tick order
// is the order block entities were added, not the order the chunk map happens
// to range in: a hopper chain that crosses a chunk border has to see the same
// order every tick, or its chained cooldown flips between moves. The chunk map
// ranges randomly, so on a world that walks it the order changes between
// calls.
func TestBlockEntitiesKeepAddOrderAcrossChunks(t *testing.T) {
	t.Parallel()
	w := newTestWorld(t, NewFlatGenerator())
	for _, pos := range []ChunkPos{{X: 0, Z: 0}, {X: 1, Z: 0}, {X: 0, Z: 1}, {X: 1, Z: 1}} {
		w.Acquire(pos)
	}

	added := []*BlockEntity{
		w.EnsureBlockEntity(4, GroundLevel, 4, KindHopper),
		w.EnsureBlockEntity(20, GroundLevel, 4, KindHopper),
		w.EnsureBlockEntity(4, GroundLevel, 20, KindHopper),
		w.EnsureBlockEntity(20, GroundLevel, 20, KindHopper),
	}
	for i, be := range added {
		if be == nil {
			t.Fatalf("could not place a hopper for entry %d", i)
		}
	}

	want := entityPositions(added)
	for call := range 50 {
		if got := entityPositions(w.TickingBlockEntities()); !slices.Equal(got, want) {
			t.Fatalf("call %d: tick order = %v, want the order the entities were added %v", call, got, want)
		}
	}
}

// TestAReplacedBlockEntityGoesToTheEndOfTheChunkOrder checks that swapping a
// position's block entity for another kind re-registers it last, as vanilla's
// removeBlockEntity then addBlockEntityTicker does, rather than keeping the
// old entity's place.
func TestAReplacedBlockEntityGoesToTheEndOfTheChunkOrder(t *testing.T) {
	t.Parallel()
	c := newChunk(0, 0)
	first := newBlockEntity(KindChest, 0, GroundLevel, 0)
	second := newBlockEntity(KindChest, 4, GroundLevel, 0)
	c.setBlockEntity(first)
	c.setBlockEntity(second)

	replacement := newBlockEntity(KindBarrel, 0, GroundLevel, 0)
	c.setBlockEntity(replacement)

	got := entityPositions(c.blockEntities())
	want := [][3]int32{{4, GroundLevel, 0}, {0, GroundLevel, 0}}
	if !slices.Equal(got, want) {
		t.Fatalf("chunk order after the swap = %v, want the untouched chest then the replacement %v", got, want)
	}
}

// TestUnloadingAChunkFlagsItsBlockEntitiesRemoved checks that a chunk leaving
// the world flags every block entity it takes with it, so a turn whose list
// was read before the unload finds the entity gone instead of acting on it.
func TestUnloadingAChunkFlagsItsBlockEntitiesRemoved(t *testing.T) {
	t.Parallel()
	w := newTestWorld(t, NewFlatGenerator())
	pos := ChunkPos{X: 0, Z: 0}
	w.Acquire(pos)
	be := w.EnsureBlockEntity(4, GroundLevel, 4, KindHopper)
	if be == nil {
		t.Fatal("could not place a hopper")
	}

	w.Release(pos)
	w.FlushSaves()

	live := false
	be.UseLive(func(*BlockEntity) { live = true })
	if live {
		t.Error("a UseLive turn ran on a hopper whose chunk unloaded")
	}
}

// TestReplacingABlockEntityFlagsTheOldOneRemoved checks that a block entity
// swapped for another kind is flagged removed, so a turn still holding the
// old pointer skips.
func TestReplacingABlockEntityFlagsTheOldOneRemoved(t *testing.T) {
	t.Parallel()
	w := newTestWorld(t, NewFlatGenerator())
	pos := ChunkPos{X: 0, Z: 0}
	w.Acquire(pos)
	old := w.EnsureBlockEntity(4, GroundLevel, 4, KindChest)
	if old == nil {
		t.Fatal("could not place a chest")
	}
	if w.EnsureBlockEntity(4, GroundLevel, 4, KindBarrel) == nil {
		t.Fatal("could not place the replacement")
	}

	live := false
	old.UseLive(func(*BlockEntity) { live = true })
	if live {
		t.Error("a UseLive turn ran on a block entity replaced by another kind")
	}
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

// TestTickingBlockEntitiesMatchTheFilteredOrder checks that the ticking list
// stays blockEntityOrder filtered by blockEntityTicks, in order, through a
// swap of a position's entity, an unload and a reload.
func TestTickingBlockEntitiesMatchTheFilteredOrder(t *testing.T) {
	t.Parallel()
	w := newTestWorld(t, NewFlatGenerator())
	pos := ChunkPos{X: 0, Z: 0}
	w.Acquire(pos)
	check := func(step string) {
		t.Helper()
		w.mu.RLock()
		var want []*BlockEntity
		for _, be := range w.blockEntityOrder {
			if blockEntityTicks(be.Kind) {
				want = append(want, be)
			}
		}
		got := slices.Clone(w.tickingBlockEntities)
		w.mu.RUnlock()
		if !slices.Equal(got, want) {
			t.Fatalf("%s: ticking = %v, want %v", step, entityPositions(got), entityPositions(want))
		}
	}
	add := func(x int32, kind BlockEntityKind) {
		t.Helper()
		if w.EnsureBlockEntity(x, GroundLevel, 4, kind) == nil {
			t.Fatalf("could not place a %s", kind)
		}
	}
	add(1, KindChest)
	add(2, KindHopper)
	add(3, KindChest)
	add(4, KindFurnace)
	check("placed")
	if n := len(w.TickingBlockEntities()); n != 2 {
		t.Fatalf("ticking entities = %d, want the hopper and the furnace", n)
	}
	add(2, KindChest)
	check("hopper swapped for a chest")
	add(2, KindHopper)
	check("chest swapped back")
	w.Release(pos)
	check("unloaded")
	w.Acquire(pos)
	check("reloaded")
	w.Release(pos)
}
