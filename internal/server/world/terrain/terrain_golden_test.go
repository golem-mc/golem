package terrain

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/server/world/levelgen"
)

// The values in testdata/terrain_golden.txt come from the vanilla classes on
// the 26.3 server jar (tools/java/TerrainGolden.java, mise run
// gen:terrain-golden); the grammar is in that file's header.
func TestBuildTerrainGolden(t *testing.T) {
	testBuildTerrainGolden(t, "testdata/terrain_golden.txt", false)
}

func TestBuildOverworldTerrainGolden(t *testing.T) {
	testBuildTerrainGolden(t, "testdata/overworld_terrain_golden.txt", true)
}

func testBuildTerrainGolden(t *testing.T, path string, natural bool) {
	t.Helper()
	g := readGolden(t, path)
	columns, heights := map[string]string{}, map[string]string{}
	for _, r := range g["terraincolumn"] {
		columns[strings.Join(r.fields, " ")] = r.values[0]
	}
	for _, r := range g["terrainheight"] {
		heights[strings.Join(r.fields, " ")] = strings.Join(r.values, " ")
	}
	dims := map[string]Dimension{"overworld": Overworld, "nether": Nether, "end": End}
	fixed := map[string]string{"overworld": "plains", "nether": "nether_wastes", "end": "the_end"}
	carvers := map[string][]Carver{"overworld": OverworldCarvers, "nether": NetherCarvers}
	states := map[string]*RandomState{}
	melt := map[string]string{}
	for _, r := range g["terrainmelt"] {
		melt[strings.Join(r.fields, " ")] = r.values[0]
	}
	if natural && len(g["terrain"]) != 42 {
		t.Fatalf("got %d chunks, want 42", len(g["terrain"]))
	}
	postRecords, maskRecords := map[string]string{}, map[string]string{}
	for _, r := range g["terrainpost"] {
		postRecords[strings.Join(r.fields, " ")] = strings.Join(r.values, " ")
	}
	for _, r := range g["terrainmask"] {
		maskRecords[strings.Join(r.fields, " ")] = strings.Join(r.values, " ")
	}
	for _, r := range g["terrain"] {
		t.Run(strings.Join(r.fields, "/"), func(t *testing.T) {
			dim, seed := r.fields[0], goldenSeed(t, r.fields[1])
			cx, cz := goldenInt(t, r.fields[2]), goldenInt(t, r.fields[3])
			key := dim + r.fields[1]
			if states[key] == nil {
				states[key] = NewRandomState(dims[dim], seed)
			}
			rs := states[key]
			minY, height := int32(0), int32(256)
			if dim == "overworld" {
				minY, height = -64, 384
			}
			chunk := &postProcessingGoldenChunk{ProtoChunk: NewProtoChunk(minY, height, rs.Materials.defaultMat), post: make([][]uint16, height/16)}
			zoom := levelgen.BiomeZoomSeed(seed)
			biomes := TerrainBiomes{
				Surface: &goldenBiomeSource{names: goldenBiomes[dim], zoom: zoom, melt: strings.Repeat("0", 256)},
				Carving: &goldenBiomeSource{names: []string{fixed[dim]}, zoom: zoom, melt: strings.Repeat("0", 256)},
				Carvers: func(int32, int32) []Carver { return carvers[dim] },
			}
			recordKey := strings.Join(r.fields, " ")
			if natural {
				surface := newNaturalGoldenBiomes(rs, melt[recordKey], false)
				carving := newNaturalGoldenBiomes(rs, melt[recordKey], true)
				biomes.Surface, biomes.Carving = surface, carving
				biomes.Carvers = carving.carvers
			}
			mask := rs.BuildTerrain(chunk, cx, cz, nil, biomes)
			if got, want := chunk.postDigest(), postRecords[recordKey]; got != want {
				t.Errorf("post-processing count/digest %s, want %s", got, want)
			}
			if got, want := carvingMaskDigest(mask, minY, height), maskRecords[recordKey]; got != want {
				t.Errorf("carving mask count/digest %s, want %s", got, want)
			}

			h := sha256.New()
			for x := range int32(16) {
				for z := range int32(16) {
					for y := minY; y < minY+height; y++ {
						h.Write([]byte(chunk.Get(x, y, z).Name() + "\n"))
					}
				}
			}
			if got := hex.EncodeToString(h.Sum(nil)); got != r.values[0] {
				t.Errorf("digest %s, want %s", got, r.values[0])
				for _, c := range [][2]int32{{0, 0}, {7, 9}, {15, 15}} {
					key := fmt.Sprintf("%s %d %d", strings.Join(r.fields, " "), c[0], c[1])
					if got, want := rleColumn(chunk.ProtoChunk, c[0], c[1]), columns[key]; got != want {
						t.Errorf("column %d,%d: %s\nwant %s", c[0], c[1], got, want)
					}
				}
			}
			var tops []string
			for z := range int32(16) {
				for x := range int32(16) {
					tops = append(tops, strconv.Itoa(int(chunk.Height(x, z))))
				}
			}
			if got, want := strings.Join(tops, " "), heights[strings.Join(r.fields, " ")]; got != want {
				t.Errorf("WORLD_SURFACE_WG heights\n got %s\nwant %s", got, want)
			}
		})
	}
}

type postProcessingGoldenChunk struct {
	*ProtoChunk
	post [][]uint16
}

func (c *postProcessingGoldenChunk) MarkPosForPostProcessing(x, y, z int32) {
	section := (y - c.MinY()) >> 4
	c.post[section] = append(c.post[section], uint16(x&15|(y&15)<<4|(z&15)<<8))
}

func (c *postProcessingGoldenChunk) postDigest() string {
	h, count := sha256.New(), 0
	for section, offsets := range c.post {
		for _, offset := range offsets {
			fmt.Fprintf(h, "%d %d\n", section, offset)
			count++
		}
	}
	return fmt.Sprintf("%d %x", count, h.Sum(nil))
}

func carvingMaskDigest(mask *CarvingMask, minY, height int32) string {
	data, count := make([]byte, 256*height), 0
	for x := range int32(16) {
		for z := range int32(16) {
			for y := minY; y < minY+height; y++ {
				if mask.Get(x, y, z) {
					data[(x*16+z)*height+y-minY] = 1
					count++
				}
			}
		}
	}
	return fmt.Sprintf("%d %x", count, sha256.Sum256(data))
}

func TestCarvingMaskGolden(t *testing.T) {
	g := readGolden(t, "testdata/terrain_golden.txt")
	states := goldenStates{}
	carvers := map[string][]Carver{"overworld": OverworldCarvers, "nether": NetherCarvers}
	if len(g["terrainmask"]) == 0 {
		t.Fatal("no terrainmask records")
	}
	for _, r := range g["terrainmask"] {
		rs := states.get(t, r.fields[0], r.int(t, 1))
		cx, cz := int32(r.int(t, 2)), int32(r.int(t, 3))
		mask := rs.CarvingMask(cx, cz, func(int32, int32) []Carver { return carvers[r.fields[0]] })
		height := int32(256)
		if r.fields[0] == "overworld" {
			height = 384
		}
		if got, want := carvingMaskDigest(mask, rs.MinY, height), strings.Join(r.values, " "); got != want {
			t.Errorf("%s: count/digest %s, want %s", r, got, want)
		}
	}
}
