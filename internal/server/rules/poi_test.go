package rules

import (
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
