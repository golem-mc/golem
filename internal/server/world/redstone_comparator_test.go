package world

import (
	"slices"
	"testing"

	"github.com/golem-mc/golem/registry"
)

func TestContainerChangesRequireALoadedComparator(t *testing.T) {
	t.Parallel()
	w := newTestWorld(t, NewFlatGenerator())
	w.SetDropUntouchedChunks(true)
	w.Acquire(ChunkPos{})
	pos := ChunkPos{X: 1}
	w.Acquire(pos)
	container := posKey{15, GroundLevel, 0}
	touch := func() { w.Touch(container[0], container[1], container[2]) }
	check := func(step string, want ...containerChange) {
		t.Helper()
		got := w.takeContainerChanges()
		if !slices.Equal(got, want) {
			t.Fatalf("%s: drained %v, want %v", step, got, want)
		}
	}

	w.SetWorldAge(4)
	touch()
	if len(w.containerChanges) != 0 {
		t.Fatal("touch queued a wake with no comparator loaded")
	}
	if !w.ChunkDirty(ChunkPos{}) {
		t.Fatal("touch did not dirty the container chunk")
	}
	w.Save()
	colliding := ChunkPos{X: nearSlots}
	w.Acquire(colliding)
	touch()
	check("no comparator")
	if !w.ChunkDirty(ChunkPos{}) || w.ChunkDirty(colliding) {
		t.Fatal("touch on a near-cache miss did not dirty only the container chunk")
	}
	w.Release(colliding)

	west, ok := registry.WithProp(registry.BlockComparator.DefaultState(), registry.PropFacing, "west")
	if !ok {
		t.Fatal("missing west-facing comparator state")
	}
	w.SetWorldAge(10)
	touch()
	w.SetBlock(16, GroundLevel, 0, west)
	if w.BlockEntity(16, GroundLevel, 0) != nil {
		t.Fatal("expected a comparator block without a block entity")
	}
	w.TickTime()
	touch()
	check("first comparator placed after touch", containerChange{pos: container, tick: 11})
	check("already drained")

	north, ok := registry.WithProp(west, registry.PropFacing, "north")
	if !ok || north == west {
		t.Fatal("missing north-facing comparator state")
	}
	w.SetBlock(16, GroundLevel, 0, north)
	touch()
	change := containerChange{pos: container, tick: 11}
	check("comparator changed facing", change)

	w.SetBlocks([]BlockChange{{X: 16, Y: GroundLevel, State: airState}})
	touch()
	check("removed last comparator in a batch")

	w.SetBlocks([]BlockChange{{X: 16, Y: GroundLevel, State: west}})
	touch()
	check("placed comparator in a batch", change)
	w.Release(pos)
	w.FlushSaves()
	if w.ChunkLoaded(pos) {
		t.Fatal("dirty comparator chunk stayed loaded")
	}
	touch()
	check("unloaded after saving")

	w.Acquire(pos)
	touch()
	check("loaded saved comparator", change)
	w.Release(pos)
	if w.ChunkLoaded(pos) {
		t.Fatal("clean comparator chunk stayed loaded")
	}
	touch()
	check("unloaded clean chunk")
}

type comparatorTestGenerator struct{ *FlatGenerator }

func (g comparatorTestGenerator) Generate(c *Chunk) {
	g.FlatGenerator.Generate(c)
	if c.X() == 1 {
		c.Fill(80, 96, registry.BlockComparator.DefaultState())
	}
}

func TestContainerChangesSeeGeneratedComparators(t *testing.T) {
	t.Parallel()
	w := newTestWorld(t, comparatorTestGenerator{NewFlatGenerator()})
	w.Acquire(ChunkPos{})
	pos := ChunkPos{X: 1}
	w.Acquire(pos)
	check := func(step string, y int32, want int) {
		t.Helper()
		w.Touch(15, y, 0)
		if got := w.takeContainerChanges(); len(got) != want {
			t.Fatalf("%s: drained %v, want %d changes", step, got, want)
		}
	}
	check("loaded generated comparator section", 80, 1)
	check("loaded, other section", GroundLevel, 0)
	w.Release(pos)
	w.FlushSaves()
	w.Acquire(pos)
	check("reloaded uniform comparator section", 80, 1)
	check("reloaded, other section", GroundLevel, 0)
}

func TestContainerChangesReachAcrossChunkBorders(t *testing.T) {
	t.Parallel()
	w := newTestWorld(t, NewFlatGenerator())
	for cx := int32(-1); cx <= 1; cx++ {
		w.Acquire(ChunkPos{X: cx})
	}
	comparator := registry.BlockComparator.DefaultState()
	check := func(step string, container posKey, want int) {
		t.Helper()
		w.Touch(container[0], container[1], container[2])
		if got := w.takeContainerChanges(); len(got) != want {
			t.Fatalf("%s: drained %v, want %d changes", step, got, want)
		}
	}
	y := int32(GroundLevel)
	w.SetBlock(-2, y, 0, comparator)
	check("negative side, within three", posKey{1, y, 0}, 1)
	check("negative side, out of reach", posKey{3, y, 0}, 0)
	w.SetBlock(-2, y, 0, airState)
	w.SetBlock(16, y, 0, comparator)
	check("positive border from the lower chunk", posKey{13, y, 0}, 1)
	check("positive border, out of reach", posKey{12, y, 0}, 0)
	check("outside the world", posKey{13, 10000, 0}, 0)

	// A comparator placed after the touch is recorded still wakes.
	w.SetBlock(16, y, 0, airState)
	w.SetBlock(-16, y, 0, comparator)
	w.Touch(13, y, 0)
	w.SetBlock(14, y, 0, comparator)
	if got := w.takeContainerChanges(); len(got) != 1 {
		t.Fatalf("comparator placed before drain: drained %v, want one change", got)
	}
}
