package world

import (
	"sync"
	"sync/atomic"
	"testing"

	"github.com/golem-mc/golem/internal/server/world/levelgen"
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

func TestMapStructureSearchKeepsPerimeterOrderAndSkipsReservedStarts(t *testing.T) {
	g := NewOverworldGenerator(0)
	id := "minecraft:village_plains"
	set := &levelgen.StructureSet{Entries: []levelgen.StructureSetEntry{{Structure: id, Weight: 1}}, Placement: levelgen.StructurePlacement{Spacing: 1, Frequency: 1, LocateOffset: [3]int32{9, 0, 9}}}
	for x := int32(-1); x <= 1; x++ {
		for z := int32(-1); z <= 1; z++ {
			g.structures.noStarts[[2]int32{x, z}] = struct{}{}
		}
	}
	for _, p := range [][2]int32{{-1, -1}, {-1, 0}} {
		g.structures.starts[p] = []*structureStart{{def: levelgen.StructureNamed(id)}}
	}
	x, z, _, ok := g.randomSpreadStructure(set, []string{id}, 0, 0, 1, nil)
	if !ok || x != -7 || z != -7 {
		t.Fatalf("scan chose (%d,%d), want first corner plus locate offset", x, z)
	}
	var visited [][2]int32
	x, z, _, ok = g.randomSpreadStructure(set, []string{id}, 0, 0, 1, func(_ string, cx, cz int32) bool {
		visited = append(visited, [2]int32{cx, cz})
		return len(visited) > 1
	})
	if !ok || x != -7 || z != 9 || len(visited) != 2 || visited[0] != [2]int32{-1, -1} {
		t.Fatalf("skip chose (%d,%d), visited %v", x, z, visited)
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

func TestStrongholdReservationCanReadTheRingCache(t *testing.T) {
	g := NewOverworldGenerator(12345)
	calls := 0
	_, _, id, ok := g.locateStructure([]string{strongholdID}, 0, 64, 0, 0, func(_ string, x, z int32) bool {
		calls++
		return g.inStrongholdRing(x, z)
	})
	if !ok || id != strongholdID || calls == 0 {
		t.Fatal("stronghold reference callback failed")
	}
}

func TestExplorerReferenceRejectsEmptyAndInvalidStarts(t *testing.T) {
	w, err := NewWorld(t.TempDir(), &mapTicketGenerator{FlatGenerator: NewFlatGenerator()})
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	pos := ChunkPos{}
	c, _ := w.Acquire(pos)
	defer w.Release(pos)
	for _, id := range []string{"INVALID", "minecraft:monument"} {
		w.mu.Lock()
		c.setLiveStructureStart("minecraft:monument", func() nbt.Compound { return structureStartNBT(id, 0, 0, nil) })
		w.mu.Unlock()
		if w.referenceMapStructure("minecraft:monument", 0, 0) {
			t.Fatalf("reserved invalid or empty start %s", id)
		}
	}
}

func TestMapSearchClaimsTheStructureAtTheOrigin(t *testing.T) {
	t.Parallel()
	const id = "minecraft:village_plains"
	ids := []string{id}
	start := ChunkPos{X: 17, Z: 59}
	g := NewOverworldGenerator(0)
	first := newChunk(start.X, start.Z)
	g.saveStructures(first)
	structures, _ := nbt.Get[nbt.Compound](first.savedRoot(), "structures")
	starts, _ := nbt.Get[nbt.Compound](structures, "starts")
	tag, _ := nbt.Get[nbt.Compound](starts, id)
	children, _ := nbt.Get[nbt.List](tag, "Children")
	bb := children.Items[0].(nbt.Compound)["BB"].(nbt.IntArray)
	x, y, z := bb[0], bb[1], bb[2]
	build := func() *World {
		pos := ChunkPos{X: x >> 4, Z: z >> 4}
		c := newChunk(pos.X, pos.Z)
		g.saveStructures(c)
		w := newTestWorld(t, g)
		w.mu.Lock()
		w.putChunk(pos, c, nil, false)
		w.mu.Unlock()
		return w
	}
	refs := func(w *World) int32 {
		c, _ := w.Acquire(start)
		defer w.Release(start)
		w.mu.RLock()
		defer w.mu.RUnlock()
		structures, _ := nbt.Get[nbt.Compound](c.savedRoot(), "structures")
		starts, _ := nbt.Get[nbt.Compound](structures, "starts")
		s, _ := nbt.Get[nbt.Compound](starts, id)
		n, _ := nbt.IntField(s, "references")
		return n
	}
	w := build()
	if _, _, _, ok := w.FindMapStructure(ids, x, y, z, 0, true); ok {
		t.Fatal("the claimed village was found")
	}
	if n := refs(w); n != 1 {
		t.Fatalf("references = %d, want 1", n)
	}
	if fx, fz, fid, ok := w.FindMapStructure(ids, x, y, z, 0, false); !ok || fx != 272 || fz != 944 || fid != id {
		t.Fatalf("ordinary search = (%d,%d) %s %v", fx, fz, fid, ok)
	}
	if n := refs(w); n != 1 {
		t.Fatalf("references = %d after ordinary search, want 1", n)
	}
	w = build()
	if fx, fz, fid, ok := w.FindMapStructure(ids, x, y+500, z, 0, true); !ok || fx != 272 || fz != 944 || fid != id {
		t.Fatalf("origin above the box = (%d,%d) %s %v", fx, fz, fid, ok)
	}
}
