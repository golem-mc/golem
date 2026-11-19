package rules

import (
	"math/rand/v2"
	"testing"

	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

func TestPOIKindOfABedHeadAndFootAndEachCauldron(t *testing.T) {
	for _, part := range []string{"head", "foot"} {
		state, ok := registry.FindState("minecraft:blue_bed", map[string]string{"part": part})
		if !ok {
			t.Fatal("no blue bed state")
		}
		want := POINone
		if part == "head" {
			want = POIHome
		}
		if got := POIKindOf(state); got != want {
			t.Errorf("bed %s: kind %d, want %d", part, got, want)
		}
	}
	for _, name := range []string{"minecraft:cauldron", "minecraft:water_cauldron", "minecraft:lava_cauldron", "minecraft:powder_snow_cauldron"} {
		state, ok := registry.FindState(name, nil)
		if !ok {
			t.Fatalf("no %s state", name)
		}
		if got := POIKindOf(state); got != POILeatherworker || JobSiteProfession(got) != "minecraft:leatherworker" {
			t.Errorf("%s: kind %d", name, got)
		}
	}
	bell, _ := registry.FindState("minecraft:bell", nil)
	stone, _ := registry.FindState("minecraft:stone", nil)
	if POIKindOf(bell) != POIMeeting || IsVillagePOI(stone) || JobSiteProfession(POIMeeting) != "" {
		t.Error("bell, stone or the non-job profession wrong")
	}
}

func TestVillageDistanceUsesOccupiedSections(t *testing.T) {
	lectern, _ := registry.BlockByName("minecraft:lectern")
	w := &occupiedPOIChunks{state: lectern.DefaultState(), loaded: true}
	v := NewVillageIndex(w)
	if got := v.SectionsToVillage(0, 64, 0, 6); got != 7 {
		t.Fatalf("unclaimed workstation: distance %d, want 7", got)
	}
	claim := []POI{{X: 48, Y: 64, Z: 0}}
	v.SetOccupied(claim)
	if got := v.SectionsToVillage(0, 64, 0, 6); got != 3 {
		t.Errorf("claimed workstation: distance %d, want 3", got)
	}
	if got := v.SectionsToVillage(0, 64, 0, 1); got != 2 {
		t.Errorf("capped distance %d, want 2", got)
	}
	v.SetOccupied(nil)
	if got := v.SectionsToVillage(0, 64, 0, 6); got != 7 {
		t.Errorf("released ticket: distance %d, want 7", got)
	}
	w.state = 0
	v.SetOccupied(claim)
	if got := v.SectionsToVillage(0, 64, 0, 6); got != 7 {
		t.Errorf("removed workstation: distance %d, want 7", got)
	}
	w.state, w.loaded = lectern.DefaultState(), false
	v.SetOccupied(claim)
	if got := v.SectionsToVillage(0, 64, 0, 6); got != 7 {
		t.Errorf("unloaded workstation: distance %d, want 7", got)
	}
}

type occupiedPOIChunks struct {
	state  uint16
	loaded bool
}

func (w *occupiedPOIChunks) ChunkLoaded(world.ChunkPos) bool { return w.loaded }
func (*occupiedPOIChunks) SurfaceHeight(int32, int32) int32  { return 64 }
func (*occupiedPOIChunks) MinY() int32                       { return 0 }
func (*occupiedPOIChunks) MaxY() int32                       { return 128 }
func (w *occupiedPOIChunks) BlockAt(x, y, z int32) uint16 {
	if x == 48 && y == 64 && z == 0 {
		return w.state
	}
	return 0
}

func TestVillageDistanceBoundedNeighborhood(t *testing.T) {
	v := NewVillageIndex(&occupiedPOIChunks{})
	v.SetOccupied(nil)
	// Distant sections must not affect any local query.
	for i := range 10000 {
		v.occupied[[3]int{100 + i, -100 - i, 100 + i}] = struct{}{}
	}
	for _, offset := range [][3]int{{-6, -6, -6}, {6, 6, 6}, {-6, 0, 6}, {0, -6, 0}, {0, 0, 0}} {
		key := [3]int{offset[0] - 2, offset[1] + 4, offset[2] - 1}
		v.occupied[key] = struct{}{}
		for _, limit := range []int{0, 1, 5, 6} {
			want := min(limit+1, max(abs(offset[0]), abs(offset[1]), abs(offset[2])))
			if got := v.SectionsToVillage(-17, 64, -1, limit); got != want {
				t.Errorf("offset %v limit %d: got %d, want %d", offset, limit, got, want)
			}
		}
		delete(v.occupied, key)
	}
	if got := v.SectionsToVillage(-17, 64, -1, 6); got != 7 {
		t.Errorf("distant sections: got %d, want 7", got)
	}
}

func TestVillageDistanceWalkAndProbeAgree(t *testing.T) {
	rng := rand.New(rand.NewPCG(7, 7))
	v := NewVillageIndex(nil)
	v.occupied = map[[3]int]struct{}{}
	for range 40 {
		v.occupied[[3]int{rng.IntN(31) - 15, rng.IntN(31) - 15, rng.IntN(31) - 15}] = struct{}{}
	}
	check := func() {
		for _, limit := range []int{0, 1, 5, 6} {
			for range 50 {
				x, y, z := rng.IntN(300)-150, rng.IntN(300)-150, rng.IntN(300)-150
				want := limit + 1
				for k := range v.occupied {
					want = min(want, max(abs(k[0]-x>>4), abs(k[1]-y>>4), abs(k[2]-z>>4)))
				}
				if got := v.SectionsToVillage(x, y, z, limit); got != want {
					t.Fatalf("limit %d at %d,%d,%d: got %d, want %d", limit, x, y, z, got, want)
				}
			}
		}
	}
	check()
	for i := range 3000 {
		v.occupied[[3]int{1000 + i, 1000, 1000}] = struct{}{}
	}
	check()
}
