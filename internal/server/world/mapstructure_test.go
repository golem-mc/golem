package world

import (
	"sync"
	"sync/atomic"
	"testing"

	"github.com/golem-mc/golem/nbt"
)

func TestExplorerDestinationsIncludeVillagesMonumentsAndMansions(t *testing.T) {
	t.Parallel()
	g := NewOverworldGenerator(0)
	for _, tc := range []struct {
		id   string
		x, z int32
	}{
		{"minecraft:village_plains", 17, 59},
		{"minecraft:monument", -71, -15},
		{"minecraft:mansion", -221, -52},
	} {
		x, z, id, ok := g.NearestStructure([]string{tc.id}, tc.x*16, 64, tc.z*16, 0)
		if !ok || x != tc.x*16 || z != tc.z*16 || id != tc.id {
			t.Fatalf("%s: (%d,%d) %s found=%v", tc.id, x, z, id, ok)
		}
	}
	if len(g.protos.mem) != 0 {
		t.Fatal("destination search decorated terrain")
	}
	if _, _, _, ok := g.NearestStructure([]string{"minecraft:village_plains"}, 0, 64, 0, -1); ok {
		t.Fatal("negative radius searched a placement")
	}
	g.DisableStructures()
	if _, _, _, ok := g.NearestStructure([]string{"minecraft:village_plains"}, 272, 64, 944, 0); ok {
		t.Fatal("disabled generation still found a village")
	}
}

// mapTicketGenerator supplies two cheap starts whose live fields can change after reservation.
type mapTicketGenerator struct {
	*FlatGenerator
	marker atomic.Int32
}

func (g *mapTicketGenerator) Generate(c *Chunk) {
	g.FlatGenerator.Generate(c)
	c.setLiveStructureStart("minecraft:monument", func() nbt.Compound {
		tag := structureStartNBT("minecraft:monument", c.x, c.z, []nbt.Tag{nbt.Compound{"marker": nbt.Int(g.marker.Load())}})
		return tag
	})
}
func (g *mapTicketGenerator) locateStructure(ids []string, _, _, _ int32, _ int, accept func(string, int32, int32) bool) (int32, int32, string, bool) {
	if !containsID(ids, "minecraft:monument") {
		return 0, 0, "", false
	}
	for x := range int32(2) {
		if accept == nil || accept("minecraft:monument", x, 0) {
			return x * 16, 0, "minecraft:monument", true
		}
	}
	return 0, 0, "", false
}

func TestExplorerReferencesAreAtomicSavedAndKeepLiveStarts(t *testing.T) {
	dir := t.TempDir()
	g := &mapTicketGenerator{FlatGenerator: NewFlatGenerator()}
	open := func() *World {
		w, err := NewWorld(dir, g)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = w.Close() })
		return w
	}
	w := open()
	ids := []string{"minecraft:monument"}
	// Ordinary locate does not spend either ticket.
	if x, _, _, ok := w.FindMapStructure(ids, 0, 64, 0, 50, false); !ok || x != 0 {
		t.Fatal("ordinary lookup failed")
	}
	w.Acquire(ChunkPos{})
	w.Acquire(ChunkPos{X: 1})
	var results sync.Map
	var wg sync.WaitGroup
	var successes atomic.Int32
	for range 16 {
		wg.Go(func() {
			if x, _, _, ok := w.FindMapStructure(ids, 0, 64, 0, 50, true); ok {
				if _, loaded := results.LoadOrStore(x, true); loaded {
					t.Error("two maps reserved the same destination")
				}
				successes.Add(1)
			}
		})
	}
	wg.Wait()
	if successes.Load() != 2 {
		t.Fatalf("reserved %d starts, want two", successes.Load())
	}
	g.marker.Store(42)
	w.Release(ChunkPos{})
	w.Release(ChunkPos{X: 1})
	w.FlushSaves()
	w.Close()
	w = open()
	if _, _, _, ok := w.FindMapStructure(ids, 0, 64, 0, 50, true); ok {
		t.Fatal("restart forgot reference tickets")
	}
	c, _ := w.Acquire(ChunkPos{})
	structures, _ := nbt.Get[nbt.Compound](c.savedRoot(), "structures")
	starts, _ := nbt.Get[nbt.Compound](structures, "starts")
	start, _ := nbt.Get[nbt.Compound](starts, "minecraft:monument")
	children, _ := nbt.Get[nbt.List](start, "Children")
	refs, _ := nbt.IntField(start, "references")
	marker, _ := nbt.IntField(children.Items[0].(nbt.Compound), "marker")
	if refs != 1 || marker != 42 {
		t.Fatalf("saved reference=%d marker=%d, want 1 and 42", refs, marker)
	}
	w.Release(ChunkPos{})
	if x, _, _, ok := w.FindMapStructure(ids, 0, 64, 0, 50, false); !ok || x != 0 {
		t.Fatal("ordinary locate hid a reserved destination")
	}
}
