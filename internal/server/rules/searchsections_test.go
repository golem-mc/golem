package rules

import (
	"reflect"
	"testing"

	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

type countedSearchWorld struct {
	*world.World
	reads int
}

func (w *countedSearchWorld) BlockAt(x, y, z int32) uint16 {
	w.reads++
	return w.World.BlockAt(x, y, z)
}

func newSearchWorld(t *testing.T) *countedSearchWorld {
	t.Helper()
	w, err := world.NewWorld(t.TempDir(), world.NewFlatGenerator())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { w.Close() })
	w.Acquire(world.ChunkPos{})
	return &countedSearchWorld{World: w}
}

func TestPortalSectionSkipsPreserveSelectionAndColumnFoot(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name                   string
		ys                     []int32
		atY, minY, maxY, wantY int32
	}{
		{"empty gaps", []int32{-48, -47, 0, 1, 2}, 1, -64, 319, 0},
		{"continuous section boundary", []int32{15, 16, 17}, 17, -64, 319, 15},
		{"negative section boundary", []int32{-17, -16, -15}, -15, -64, 319, -17},
		{"lower Y breaks distance tie", []int32{-1, 1}, 0, -64, 319, -1},
		{"clipped first section", []int32{15, 16, 17}, 17, 16, 18, 16},
		{"world bottom", []int32{-64}, -64, -64, 319, -64},
		{"world top", []int32{319}, 319, -64, 319, 319},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := newSearchWorld(t)
			for _, y := range tc.ys {
				w.SetBlock(1, y, 1, NetherPortalX)
			}
			fast := PortalSearch{At: world.SpawnPos{X: 1, Y: tc.atY, Z: 1}, Radius: 128}
			slow := fast
			fast.ScanChunk(w, world.ChunkPos{}, tc.minY, tc.maxY)
			if tc.maxY-tc.minY > 16 && w.reads >= 256*int(tc.maxY-tc.minY+1) {
				t.Fatal("indexed search did not skip empty sections")
			}
			slow.ScanChunk(struct{ BlockReader }{w}, world.ChunkPos{}, tc.minY, tc.maxY)
			if !reflect.DeepEqual(fast, slow) {
				t.Fatalf("indexed search %+v differs from original %+v", fast, slow)
			}
			if got, ok := fast.Found(); !ok || got != (world.SpawnPos{X: 1, Y: tc.wantY, Z: 1}) {
				t.Fatalf("found %v,%v; want foot Y %d", got, ok, tc.wantY)
			}
		})
	}
}

func TestPortalSectionSkipsPreserveHorizontalTiesAndBounds(t *testing.T) {
	t.Parallel()
	w := newSearchWorld(t)
	for _, p := range [][3]int32{{1, 64, 0}, {0, 64, 1}, {0, 64, 3}} {
		w.SetBlock(p[0], p[1], p[2], NetherPortalZ)
	}
	for _, border := range []bool{false, true} {
		s := PortalSearch{At: world.SpawnPos{Y: 64}, Radius: 2}
		want := world.SpawnPos{X: 1, Y: 64}
		if border {
			s.InBorder = func(x, z int32) bool { return x == 0 }
			want = world.SpawnPos{Y: 64, Z: 1}
		}
		s.ScanChunk(w, world.ChunkPos{}, -64, 319)
		if got, ok := s.Found(); !ok || got != want {
			t.Fatalf("found %v,%v; want %v", got, ok, want)
		}
	}
}

func TestEmptyPortalAndPOISectionsReadNoBlocks(t *testing.T) {
	t.Parallel()
	w := newSearchWorld(t)
	s := PortalSearch{Radius: 128}
	s.ScanChunk(w, world.ChunkPos{}, -64, 319)
	if _, ok := s.Found(); ok {
		t.Fatal("found portal in empty chunk")
	}
	if got := NewVillageIndex(w).InChunk(world.ChunkPos{}, 0); len(got) != 0 {
		t.Fatalf("found POIs: %v", got)
	}
	if w.reads != 0 {
		t.Fatalf("empty scans read %d blocks", w.reads)
	}
}

func TestPOISectionSkipsPreserveOrderAndChangedCache(t *testing.T) {
	t.Parallel()
	w := newSearchWorld(t)
	for _, p := range [][3]int32{{2, 31, 1}, {2, 32, 1}, {1, -16, 2}, {1, 0, 1}, {1, 319, 1}} {
		w.SetBlock(p[0], p[1], p[2], registry.BlockLectern.DefaultState())
	}
	fast := NewVillageIndex(w)
	// Hide the section index to compare with the original full scan.
	slow := NewVillageIndex(struct{ POIChunks }{w})
	got := fast.InChunk(world.ChunkPos{}, 0)
	reads := w.reads
	if want := slow.InChunk(world.ChunkPos{}, 0); !reflect.DeepEqual(got, want) || len(got) != 5 {
		t.Fatalf("indexed POIs %v, original %v", got, want)
	}
	if reads >= w.reads-reads {
		t.Fatal("POI index did not reduce block reads")
	}
	old := registry.BlockLectern.DefaultState()
	w.SetBlock(2, 32, 1, registry.BlockAir.DefaultState())
	fast.Changed(2, 32, 1, old, registry.BlockAir.DefaultState())
	slow.Changed(2, 32, 1, old, registry.BlockAir.DefaultState())
	if got, want := fast.InChunk(world.ChunkPos{}, 1), slow.InChunk(world.ChunkPos{}, 1); !reflect.DeepEqual(got, want) || len(got) != 4 {
		t.Fatalf("changed POIs %v, original %v", got, want)
	}
}
