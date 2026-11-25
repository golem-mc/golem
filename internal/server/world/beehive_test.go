package world

import (
	"slices"
	"testing"

	"github.com/golem-mc/golem/registry"
)

func TestBeehiveCellsReturnsLoadedHivesInRangeInCoordinateOrder(t *testing.T) {
	t.Parallel()
	w := newTestWorld(t, NewFlatGenerator())
	for _, pos := range []ChunkPos{{-2, -1}, {-1, -1}, {-1, 0}, {0, -1}} {
		w.Acquire(pos)
	}
	hive := registry.BlockBeehive.DefaultState()
	nest, _ := registry.WithInt(registry.BlockBeeNest.DefaultState(), registry.PropHoneyLevel, 5)
	w.SetBlock(0, -49, -1, hive)
	w.SetBlock(-1, -48, -1, nest)
	w.SetBlock(-1, -49, 0, hive)
	w.SetBlock(-1, -49, -2, hive)
	w.SetBlock(-1, worldMinY, -1, nest)
	w.SetBlock(-2, -49, 0, hive)
	w.SetBlock(-21, -49, -1, nest) // On the sphere.
	w.SetBlock(-21, -49, -2, hive) // In the box, outside the sphere.
	w.SetBlock(-22, -49, -1, hive) // Outside the box, sharing a section with hives.
	w.SetBlock(-2, -48, -2, registry.BlockBell.DefaultState())

	unloaded := ChunkPos{0, -1}
	w.Release(unloaded)
	w.FlushSaves()
	want := [][3]int32{
		{-21, -49, -1}, {-2, -49, 0}, {-1, worldMinY, -1},
		{-1, -49, -2}, {-1, -49, 0}, {-1, -48, -1},
	}
	if got := w.BeehiveCells(-1, -49, -1, 20); !slices.Equal(got, want) {
		t.Fatalf("BeehiveCells = %v, want %v", got, want)
	}

	w.Acquire(unloaded)
	want = append(want, [3]int32{0, -49, -1})
	if got := w.BeehiveCells(-1, -49, -1, 20); !slices.Equal(got, want) {
		t.Fatalf("BeehiveCells after reload = %v, want %v", got, want)
	}
}

func TestBeehiveCellsSingleHiveAllocatesOnlyTheResult(t *testing.T) {
	w := newTestWorld(t, NewFlatGenerator())
	w.Acquire(ChunkPos{1, 1})
	w.SetBlock(30, GroundLevel+2, 30, registry.BlockBeehive.DefaultState())
	if n := testing.AllocsPerRun(10, func() {
		if got := w.BeehiveCells(25, GroundLevel+1, 25, 20); len(got) != 1 {
			t.Fatalf("BeehiveCells = %v, want one hive", got)
		}
	}); n > 1 {
		t.Fatalf("BeehiveCells allocated %v times per search, want at most 1", n)
	}
}
