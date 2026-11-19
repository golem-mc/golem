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

func TestEndCityMarkerRotations(t *testing.T) {
	for rotation := RotateNone; rotation <= RotateCounterclockwise90; rotation++ {
		t.Run(rotationNames[rotation], func(t *testing.T) {
			g := NewEndGenerator(0)
			p := newEndCityPiece("ship", [3]int32{8, 100, 8}, rotation, true)
			s := &endCityStart{pieces: []*endCityPiece{p}, box: p.box}
			g.cities.Store([2]int32{0, 0}, func() *endCityStart { return s })
			var all []nbt.Compound
			ids := map[string]bool{}
			for cz := p.box.minZ >> 4; cz <= p.box.maxZ>>4; cz++ {
				for cx := p.box.minX >> 4; cx <= p.box.maxX>>4; cx++ {
					pos := ChunkPos{X: cx, Z: cz}
					tags := g.endCityEntities(pos)
					if !reflect.DeepEqual(tags, g.endCityEntities(pos)) {
						t.Fatal("entity identities changed")
					}
					for _, tag := range tags {
						id, ok := nbt.UUIDField(tag, "UUID")
						if !ok || ids[id.String()] {
							t.Fatal("missing or repeated identity across chunks")
						}
						ids[id.String()] = true
						coords := tag["Pos"].(nbt.List).Items
						if ChunkOf(float64(coords[0].(nbt.Double))) != cx || ChunkOf(float64(coords[2].(nbt.Double))) != cz {
							t.Fatal("entity belongs to another chunk")
						}
					}
					all = append(all, tags...)
				}
			}
			shulkers, frames := 0, 0
			for _, tag := range all {
				if tag["id"] == nbt.String("minecraft:shulker") {
					shulkers++
					continue
				}
				if tag["id"] != nbt.String("minecraft:item_frame") {
					t.Fatalf("unexpected entity: %v", tag)
				}
				frames++
				x, y, z := TransformTemplatePos(6, 5, 7, MirrorNone, rotation)
				if !reflect.DeepEqual(tag["block_pos"], nbt.IntArray{x + 8, y + 100, z + 8}) || tag["Facing"] != [...]nbt.Byte{3, 4, 2, 5}[rotation] {
					t.Fatalf("rotated frame: %v", tag)
				}
			}
			if shulkers != 3 || frames != 1 {
				t.Fatalf("got %d shulkers, %d frames", shulkers, frames)
			}
		})
	}
}

// TestEndCityMarkersKeepTheirCell checks that a Sentry marker sunk into the
// island leaves the terrain's end stone, as the 26.3 server does at
// (-566, 63, -2864) and (-563, 63, -2864) for seed 0.
func TestEndCityMarkersKeepTheirCell(t *testing.T) {
	c := endChunk(0, -36, -179)
	for _, x := range []int32{10, 13} {
		if got := registry.BlockOf(c.BlockAt(x, 63, 0)); got != registry.BlockEndStone {
			t.Errorf("marker cell (%d, 63, 0) = %v, want end_stone", x, got)
		}
	}
	for _, y := range []int32{62, 64} {
		if got := registry.BlockOf(c.BlockAt(10, y, 0)); got != registry.BlockAir {
			t.Errorf("cell (10, %d, 0) = %v, want air", y, got)
		}
	}
}
