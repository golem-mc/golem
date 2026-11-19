package terrain

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/server/world/levelgen"
)

// The values in testdata/material_golden.txt come from the vanilla classes on
// the 26.3 server jar (tools/java/MaterialGolden.java, mise run
// gen:material-golden); the grammar is in that file's header.

func materialGolden(t *testing.T) map[string][]goldenRecord {
	t.Helper()
	return readGolden(t, "testdata/material_golden.txt")
}

// readGolden groups a golden file's records by kind.
func readGolden(t *testing.T, path string) map[string][]goldenRecord {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	records := map[string][]goldenRecord{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		if line == "" || line[0] == '#' {
			continue
		}
		head, tail, _ := strings.Cut(line, " -> ")
		fields := strings.Fields(head)
		records[fields[0]] = append(records[fields[0]], goldenRecord{fields[0], fields[1:], strings.Fields(tail)})
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	return records
}

func goldenInt(t *testing.T, s string) int32 {
	t.Helper()
	v, err := strconv.ParseInt(s, 10, 32)
	if err != nil {
		t.Fatal(err)
	}
	return int32(v)
}

func goldenSeed(t *testing.T, s string) int64 {
	t.Helper()
	v, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func TestMaterialSystemProbesGolden(t *testing.T) {
	g := materialGolden(t)
	states := map[int64]*RandomState{}
	state := func(seed int64) *RandomState {
		if states[seed] == nil {
			states[seed] = NewRandomState(Overworld, seed)
		}
		return states[seed]
	}
	for _, r := range g["surfacedepth"] {
		seed, x, z := goldenSeed(t, r.fields[0]), goldenInt(t, r.fields[1]), goldenInt(t, r.fields[2])
		st := state(seed)
		if got := strconv.Itoa(int(st.Materials.surfaceDepth(x, z, st.random.At(0, 0, 0)))); got != r.values[0] {
			t.Errorf("surfacedepth %v = %s, want %s", r.fields, got, r.values[0])
		}
	}
	for _, r := range g["secondary"] {
		seed, x, z := goldenSeed(t, r.fields[0]), goldenInt(t, r.fields[1]), goldenInt(t, r.fields[2])
		got := fmt.Sprintf("%016x", float64Bits(float64(state(seed).Materials.surfaceSecondary.Get3D(float64(x), 0, float64(z)))))
		if got != r.values[0] {
			t.Errorf("secondary %v = %s, want %s", r.fields, got, r.values[0])
		}
	}
	for _, r := range g["band"] {
		seed, x, y, z := goldenSeed(t, r.fields[0]), goldenInt(t, r.fields[1]), goldenInt(t, r.fields[2]), goldenInt(t, r.fields[3])
		if got := state(seed).Materials.band(x, y, z).Name(); got != r.values[0] {
			t.Errorf("band %v = %s, want %s", r.fields, got, r.values[0])
		}
	}
}

// The synthetic biomes MaterialGolden.biomeName picks; the lists repeat it.
var goldenBiomes = map[string][]string{
	"overworld": {"plains", "wooded_badlands", "swamp", "mangrove_swamp", "badlands", "eroded_badlands",
		"frozen_ocean", "deep_frozen_ocean", "warm_ocean", "beach", "snowy_beach", "desert", "lukewarm_ocean", "deep_lukewarm_ocean",
		"frozen_peaks", "jagged_peaks", "snowy_slopes", "grove", "stony_peaks", "stony_shore", "windswept_hills", "dripstone_caves",
		"sulfur_caves", "windswept_savanna", "windswept_gravelly_hills", "old_growth_pine_taiga", "old_growth_spruce_taiga",
		"ice_spikes", "mushroom_fields", "dappled_forest", "forest", "taiga", "ocean"},
	"nether": {"nether_wastes", "soul_sand_valley", "crimson_forest", "warped_forest", "basalt_deltas"},
	"end":    {"the_end"},
}

// goldenBiomeSource is vanilla's BiomeManager over the synthetic biome function.
type goldenBiomeSource struct {
	names []string
	zoom  int64
	// melt marks, per column z*16+x of the chunk, a frozen ocean top that is warm
	// enough to melt an iceberg. vanilla: Biome.shouldMeltFrozenOceanIcebergSlightly
	melt string
}

func (b *goldenBiomeSource) Index(path string) int {
	for i, n := range b.names {
		if n == path {
			return i
		}
	}
	return -1
}

func (b *goldenBiomeSource) At(x, y, z int32) int {
	qx, qy, qz := levelgen.ZoomedQuart(b.zoom, x, y, z)
	idx := 5*(qx>>2) + 11*(qz>>2)
	if qy < 0 {
		idx += 7
	}
	n := int32(len(b.names))
	return int(((idx % n) + n) % n)
}

func (b *goldenBiomeSource) Temperature(_ int, x, _, z, _ int32) float32 {
	if b.melt[(z&15)*16+(x&15)] == '1' {
		return 0.2
	}
	return 0
}

// goldenHeight is MaterialGolden.height.
func goldenHeight(level, a, x, z int32) int32 {
	return level + (x*a+z*(a+1))%(3*a+2) + ((x/8)*5+(z/8)*3)%4*2
}

func buildGoldenChunk(dim string, cx, cz, level, a int32) (*ProtoChunk, Dimension) {
	switch dim {
	case "nether":
		c := NewProtoChunk(0, 256, Netherrack)
		for x := range int32(16) {
			for z := range int32(16) {
				bx, bz := cx*16+x, cz*16+z
				floor, ceiling := 20+goldenHeight(level, a, bx, bz)%60, 100+(bx*3+bz*7)&15
				for y := range int32(128) {
					switch {
					case y <= floor || y >= ceiling:
						c.Set(bx, y, bz, Netherrack)
					case y <= 32:
						c.Set(bx, y, bz, Lava)
					}
				}
			}
		}
		return c, Nether
	case "end":
		c := NewProtoChunk(0, 256, EndStone)
		for x := range int32(16) {
			for z := range int32(16) {
				bx, bz := cx*16+x, cz*16+z
				for y := int32(0); y <= 20+goldenHeight(level, a, bx, bz)%50; y++ {
					c.Set(bx, y, bz, EndStone)
				}
			}
		}
		return c, End
	}
	c := NewProtoChunk(-64, 384, Stone)
	for x := range int32(16) {
		for z := range int32(16) {
			bx, bz := cx*16+x, cz*16+z
			h := goldenHeight(level, a, bx, bz)
			for y := int32(-64); y <= max(h, 62); y++ {
				if y <= h {
					c.Set(bx, y, bz, Stone)
				} else {
					c.Set(bx, y, bz, Water)
				}
			}
		}
	}
	return c, Overworld
}

func rleColumn(c *ProtoChunk, x, z int32) string {
	var sb strings.Builder
	last, run := "", 0
	for y := c.minY; y < c.minY+c.height; y++ {
		name := c.blocks[c.index(x, y, z)].Name()
		if name == last {
			run++
			continue
		}
		if run > 0 {
			fmt.Fprintf(&sb, "%s*%d,", last, run)
		}
		last, run = name, 1
	}
	fmt.Fprintf(&sb, "%s*%d", last, run)
	return sb.String()
}

func TestMaterialSurfaceChunksGolden(t *testing.T) {
	g := materialGolden(t)
	melts := map[string]string{}
	for _, r := range g["melt"] {
		melts[strings.Join(r.fields, " ")] = r.values[0]
	}
	columns := map[string]string{}
	for _, r := range g["surfacecolumn"] {
		columns[strings.Join(r.fields, " ")] = r.values[0]
	}
	states := map[string]*RandomState{}
	for _, r := range g["surfacechunk"] {
		dim, seed := r.fields[0], goldenSeed(t, r.fields[1])
		cx, cz, level, a := goldenInt(t, r.fields[2]), goldenInt(t, r.fields[3]), goldenInt(t, r.fields[4]), goldenInt(t, r.fields[5])
		chunk, d := buildGoldenChunk(dim, cx, cz, level, a)
		key := dim + r.fields[1]
		if states[key] == nil {
			states[key] = NewRandomState(d, seed)
		}
		rs := states[key]
		melt := melts[strings.Join(r.fields[:4], " ")]
		if melt == "" {
			melt = strings.Repeat("0", 256)
		}
		biomes := &goldenBiomeSource{names: goldenBiomes[dim], zoom: levelgen.BiomeZoomSeed(seed), melt: melt}
		nc := NewNoiseChunk(rs, nil, rs.ChunkVolume(cx, cz))
		rs.Materials.BuildSurface(chunk, cx, cz, nc, biomes, nil)

		h := sha256.New()
		for x := range int32(16) {
			for z := range int32(16) {
				for y := chunk.minY; y < chunk.minY+chunk.height; y++ {
					h.Write([]byte(chunk.blocks[chunk.index(x, y, z)].Name() + "\n"))
				}
			}
		}
		if got := hex.EncodeToString(h.Sum(nil)); got != r.values[0] {
			t.Errorf("%s: digest %s, want %s", strings.Join(r.fields, " "), got, r.values[0])
			for _, c := range [][2]int32{{0, 0}, {7, 9}, {15, 15}} {
				key := fmt.Sprintf("%s %s %d %d %d %d %d %d", dim, r.fields[1], cx, cz, level, a, c[0], c[1])
				if want := columns[key]; want != rleColumn(chunk, c[0], c[1]) {
					t.Errorf("  column %d,%d: %s\n  want %s", c[0], c[1], rleColumn(chunk, c[0], c[1]), want)
				}
			}
		}
	}
}

func float64Bits(v float64) uint64 { return math.Float64bits(v) }
