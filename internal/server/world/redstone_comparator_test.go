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
	touch()
	check("no comparator")
	if w.containerChangeSeen != nil {
		t.Fatal("deduplicated a batch with no comparator")
	}
	if !w.chunks[ChunkPos{}].dirty {
		t.Fatal("touch did not dirty the container chunk")
	}

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
	w.SetWorldAge(11)
	touch()
	check("first comparator placed after touch", containerChange{pos: container, tick: 10})
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
	w.Touch(15, GroundLevel, 0)
	if got := w.takeContainerChanges(); len(got) != 1 {
		t.Fatalf("loaded generated comparator section: drained %v, want one change", got)
	}
	w.Release(pos)
	w.FlushSaves()
	w.Acquire(pos)
	w.Touch(15, GroundLevel, 0)
	if got := w.takeContainerChanges(); len(got) != 1 {
		t.Fatalf("reloaded uniform comparator section: drained %v, want one change", got)
	}
}
