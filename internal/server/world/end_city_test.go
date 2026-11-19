package world

import (
	"os"
	"reflect"
	"testing"

	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

func TestEndCityLocate(t *testing.T) {
	g := NewEndGenerator(0)
	x, z, id, ok := g.NearestStructure([]string{endCityID}, 0, 64, 0, 100)
	if !ok || id != endCityID || x != -1232 || z != -1264 {
		t.Fatalf("city = %d,%d %s %t", x, z, id, ok)
	}
	t.Logf("seed 0 nearest city: block %d,%d; chunk %d,%d", x, z, x>>4, z>>4)
	if _, _, _, ok := g.NearestStructure([]string{endCityID}, 0, 64, 0, 0); ok {
		t.Fatal("city on central island")
	}
	if _, _, _, ok := g.NearestStructure([]string{fortressID}, x, 64, z, 100); ok {
		t.Fatal("fortress in the End")
	}
	c := newChunk(x>>4, z>>4)
	g.Generate(c)

	if starts := c.StructureStarts(); len(starts) != 1 || starts[0] != endCityID {
		t.Fatalf("starts = %v", starts)
	}
	structures, _ := nbt.Get[nbt.Compound](c.savedRoot(), "structures")
	starts, _ := nbt.Get[nbt.Compound](structures, "starts")
	start, _ := nbt.Get[nbt.Compound](starts, endCityID)
	children, _ := nbt.Get[nbt.List](start, "Children")
	if len(children.Items) != 11 {
		t.Fatalf("children = %d", len(children.Items))
	}

	found := false
	for y := int32(60); y < endHeight; y++ {
		for z := range int32(16) {
			for x := range int32(16) {
				switch registry.BlockOf(c.BlockAt(x, y, z)) {
				case registry.BlockPurpurBlock:
					found = true
				case registry.BlockStructureBlock:
					t.Fatal("unprocessed marker")
				}
			}
		}
	}
	if !found {
		t.Fatal("city has no purpur blocks")
	}
}

// The starts were captured from the vanilla 26.3 parity ranges documented in
// testdata/end_city_starts.txt, including the recursive generation tags.
func TestEndCityStartsVanilla(t *testing.T) {
	data, err := os.ReadFile("testdata/end_city_starts.nbt")
	if err != nil {
		t.Fatal(err)
	}
	_, root, err := nbt.Decode(data)
	if err != nil {
		t.Fatal(err)
	}
	g := NewEndGenerator(0)
	for _, tc := range []struct {
		name   string
		cx, cz int32
	}{{"near", -77, -79}, {"ship", 26, -120}} {
		t.Run(tc.name, func(t *testing.T) {
			c := newChunk(tc.cx, tc.cz)
			g.saveStructureStarts(c)
			structures, _ := nbt.Get[nbt.Compound](c.savedRoot(), "structures")
			starts, _ := nbt.Get[nbt.Compound](structures, "starts")
			if !reflect.DeepEqual(starts[endCityID], root.(nbt.Compound)[tc.name]) {
				t.Fatal("saved start differs from vanilla")
			}
		})
	}
}

func TestEndCityShipBlocksAndReferences(t *testing.T) {
	g := NewEndGenerator(0)
	c := newChunk(25, -127)
	g.Generate(c)
	found := false
	for y := int32(125); y <= 148; y++ {
		for z := range int32(16) {
			for x := range int32(16) {
				if registry.BlockOf(c.BlockAt(x, y, z)) == registry.BlockPurpurBlock {
					found = true
				}
			}
		}
	}
	if !found {
		t.Fatal("ship has no purpur blocks")
	}
	structures, _ := nbt.Get[nbt.Compound](c.savedRoot(), "structures")
	refs, _ := nbt.Get[nbt.Compound](structures, "References")
	got, _ := nbt.Get[nbt.LongArray](refs, endCityID)
	want := nbt.LongArray{int64(uint32(26)) | int64(-120)<<32}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("references = %v, want %v", got, want)
	}
	// Overlapping pieces leave these banners unsupported in the vanilla capture.
	banner := newChunk(26, -120)
	g.Generate(banner)
	for _, p := range [][3]int32{{415, 94, -1918}, {415, 94, -1914}} {
		if state := banner.BlockAt(p[0]&15, p[1], p[2]&15); state != airState {
			t.Fatalf("unsupported banner at %v: %d", p, state)
		}
	}
	stairs := registry.MustState("minecraft:purpur_stairs", map[string]string{"facing": "south", "half": "bottom", "shape": "straight", "waterlogged": "false"})
	if got := banner.BlockAt(419&15, 71, -1919&15); got != stairs {
		t.Fatalf("settled stair = %d, want %d", got, stairs)
	}
}
