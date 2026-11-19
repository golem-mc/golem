package world

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"github.com/golem-mc/golem/internal/server/world/terrain"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/golem-mc/golem/internal/server/world/levelgen"
	"github.com/golem-mc/golem/registry"
)

// goldenTerrain is the synthetic world of tools/java/OverworldFeatureGolden.java:
// a function of (x, y, z) alone, served as shaped terrain.
type goldenTerrain struct {
	decorState
	mu     sync.Mutex
	chunks map[ChunkPos]*shapedTerrain
	states struct{ stone, deepslate, dirt, grass, water, sand, mud, clay, packedIce, snow uint16 }
	// peakTops are the mountains' top blocks; biome is what biomeIndex answers.
	peakTops       [6]uint16
	biome          terrain.OverworldBiome
	largeDripstone bool
}

func newGoldenTerrain() *goldenTerrain {
	t := &goldenTerrain{chunks: map[ChunkPos]*shapedTerrain{}}
	def := func(name string) uint16 {
		b, ok := registry.BlockByName(name)
		if !ok {
			panic(name)
		}
		return b.DefaultState()
	}
	t.states.stone, t.states.deepslate, t.states.dirt = def("minecraft:stone"), def("minecraft:deepslate"), def("minecraft:dirt")
	t.states.grass, t.states.water, t.states.sand = def("minecraft:grass_block"), def("minecraft:water"), def("minecraft:sand")
	t.states.mud, t.states.clay = def("minecraft:mud"), def("minecraft:clay")
	for i, name := range []string{"grass_block", "stone", "packed_ice", "soul_sand", "mud", "sand"} {
		t.peakTops[i] = def("minecraft:" + name)
	}
	t.states.packedIce = def("minecraft:packed_ice")
	t.states.snow = def("minecraft:snow_block")
	t.protos.init()
	return t
}

func floorMod(a, b int32) int32 { return (a%b + b) % b }

func (t *goldenTerrain) at(x, y, z int32) uint16 {
	if y < worldMinY || y >= worldMinY+worldHeight {
		return airState
	}
	// Broad caves match the large-dripstone-only terrain in the Java golden.
	if t.largeDripstone {
		if y > 70 || (y >= -50 && y <= -25) || (y >= 0 && y <= 35) || (y >= 40 && y <= 58) {
			if y < -45 {
				return t.states.water
			}
			return airState
		}
		return t.states.stone
	}
	if x < -1024 {
		if y > 70 {
			return airState
		}
		if y == 70 {
			if z < 0 {
				return t.states.snow
			}
			return t.states.sand
		}
		return t.states.stone
	}
	if x >= 256 {
		top := 90 + floorMod((x>>2)*7+(z>>2)*3, 70)
		switch {
		case y > top:
			return airState
		case y == top:
			return t.peakTops[floorMod((x>>1)+(z>>1)*3, int32(len(t.peakTops)))]
		}
		return t.states.stone
	}
	h := 60 + floorMod((x>>5)*3+(z>>5)*5, 9)
	if y > h {
		if y > 62 {
			return airState
		}
		if floorMod(x+z+y, 3) == 0 {
			return t.states.packedIce
		}
		return t.states.water
	}
	switch {
	case z&15 == 8 && (y == -40 || y == -39 || y == 5 || y == 6 || y == 30 || y == 31 || y == 45 || y == 46):
		return airState
	case y >= 10 && y <= 22 && floorMod((x>>1)+(z>>1)+floorDiv(y, 3), 3) == 0:
		return airState
	case y == h && h >= 63:
		if floorMod((x>>3)+(z>>3), 3) == 0 {
			return t.states.mud
		}
		return t.states.grass
	case y > h-4 && y >= 63:
		return t.states.dirt
	case h < 63 && y >= h-7 && y <= h-3 && floorMod((x>>3)+(z>>3), 4) == 0:
		return t.states.water
	case h < 63 && y == h-2 && floorMod(x+2*z, 7) == 0:
		return airState
	case h < 63 && floorMod((x>>2)+(z>>3), 3) == 0 && y > h-3:
		return t.states.dirt
	case h < 63 && floorMod((x>>2)+(z>>3), 3) == 1 && y == h:
		return t.states.clay
	case y > h-2:
		return t.states.sand
	case y < 0:
		return t.states.deepslate
	}
	return t.states.stone
}

func (t *goldenTerrain) cachedTerrain(cx, cz int32) *shapedTerrain {
	t.mu.Lock()
	defer t.mu.Unlock()
	pos := ChunkPos{X: cx, Z: cz}
	if s := t.chunks[pos]; s != nil {
		return s
	}
	c := newChunk(cx, cz)
	for y := int32(worldMinY); y < worldMinY+worldHeight; y++ {
		for z := range int32(16) {
			for x := range int32(16) {
				if s := t.at(cx*16+x, y, cz*16+z); s != airState {
					c.SetBlock(x, y, z, s)
				}
			}
		}
	}
	s := &shapedTerrain{}
	s.keep(c, false)
	t.chunks[pos] = s
	return s
}

func (t *goldenTerrain) terrainOf(cx, cz int32, _ *Chunk) *shapedTerrain {
	return t.cachedTerrain(cx, cz)
}
func (t *goldenTerrain) decor() *decorState                                { return &t.decorState }
func (t *goldenTerrain) isAir(state uint16) bool                           { return overworldAirBlocks[registry.BlockOf(state)] }
func (t *goldenTerrain) biomeIndex(*decorRegion, int32, int32, int32) uint { return uint(t.biome) }

func stateString(state uint16) string {
	name, _, _ := registry.BlockStateName(state)
	props := registry.StateProps(state)
	if len(props) == 0 {
		return name
	}
	keys := make([]string, 0, len(props))
	for k := range props {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	for i, k := range keys {
		keys[i] = k + "=" + props[k]
	}
	return name + "[" + strings.Join(keys, ",") + "]"
}

var featureStateText = sync.OnceValue(func() []string {
	text := make([]string, registry.StateCount)
	for state := range text {
		text[state] = stateString(uint16(state))
	}
	return text
})

type goldenChange struct {
	x, y, z int32
	state   string
}

// changes lists the blocks a region's protos hold that differ from the terrain.
func (t *goldenTerrain) changes(r *decorRegion) (string, int) {
	var out []goldenChange
	text := featureStateText()
	for i, p := range r.protos {
		if p == nil {
			continue
		}
		bx := (r.center.x + int32(i%decorRegionSpan) - decorRegionSpan/2) * 16
		bz := (r.center.z + int32(i/decorRegionSpan) - decorRegionSpan/2) * 16
		for sec, blocks := range p.blocks {
			if blocks == nil {
				continue
			}
			for index, v := range blocks {
				// A zero proto entry is unwritten, so avoid decoding its position.
				if v == 0 {
					continue
				}
				x, y, z := int32(index&15), int32(index>>8), int32(index>>4&15)
				wy := int32(sec)*16 + y + worldMinY
				if v-1 != t.at(bx+x, wy, bz+z) {
					out = append(out, goldenChange{bx + x, wy, bz + z, text[v-1]})
				}
			}
		}
	}
	slices.SortFunc(out, func(a, b goldenChange) int {
		if a.x != b.x {
			return int(a.x - b.x)
		}
		if a.y != b.y {
			return int(a.y - b.y)
		}
		return int(a.z - b.z)
	})
	h := sha256.New()
	var line [256]byte
	for _, c := range out {
		b := strconv.AppendInt(line[:0], int64(c.x), 10)
		b = strconv.AppendInt(append(b, ' '), int64(c.y), 10)
		b = strconv.AppendInt(append(b, ' '), int64(c.z), 10)
		b = append(append(append(b, ' '), c.state...), '\n')
		h.Write(b)
	}
	return hex.EncodeToString(h.Sum(nil)), len(out)
}

// TestOverworldFeatureGolden runs the ported placed features over the
// synthetic world and compares what they change with the vanilla feature
// classes' output recorded in testdata/overworld_feature_golden.txt.
func TestOverworldFeatureGolden(t *testing.T) {
	t.Parallel()
	testOverworldFeatureGolden(t, "testdata/overworld_feature_golden.txt")
}

//go:generate go run ../../../tools/gen sculk-golden

func TestSculkFeatureGolden(t *testing.T) {
	t.Parallel()
	testOverworldFeatureGolden(t, "testdata/sculk_golden.txt")
}

func testOverworldFeatureGolden(t *testing.T, path string) {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	type want struct{ n, sha string }
	hasRaw := map[string]bool{}
	expected := map[string]want{}
	nextDraws := map[string]int64{}
	results := map[string]bool{}
	counts := map[string]int{}
	var order []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) == 0 || fields[0] == "#" {
			continue
		}
		switch fields[0] {
		case "cases":
			counts[fields[1]], _ = strconv.Atoi(fields[2])
			order = append(order, fields[1])
		case "result":
			success, err := strconv.ParseBool(fields[7])
			if err != nil {
				t.Fatal(err)
			}
			results[strings.Join(fields[1:7], " ")] = success
		case "next":
			n, err := strconv.ParseInt(fields[7], 10, 64)
			if err != nil {
				t.Fatal(err)
			}
			nextDraws[strings.Join(fields[1:7], " ")] = n
		case "case", "raw":
			hasRaw[fields[1]] = hasRaw[fields[1]] || fields[0] == "raw"
			expected[strings.Join(fields[:6], " ")] = want{fields[6], fields[7]}
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	if path == "testdata/sculk_golden.txt" && (len(counts) != 2 || counts["sculk_patch_deep_dark"] != 200 || counts["sculk_patch_ancient_city"] != 200 || len(nextDraws) != 544 || len(results) != 544) {
		t.Fatalf("incomplete sculk recording: cases %v, %d random draws", counts, len(nextDraws))
	}

	g := NewOverworldGenerator(0)
	g.initOverworldFeatures()
	world := newGoldenTerrain()
	chunks := [][2]int32{{0, 0}, {-16, 32}, {48, -80}, {160, 96}}
	// freeze_top_layer's cases add a mountain chunk and cycle through biomes.
	freezeChunks := append(slices.Clone(chunks), [2]int32{320, 320})
	surfaceChunks := [][2]int32{{-80 * 16, -8 * 16}, {-80 * 16, 8 * 16}, {-96 * 16, -24 * 16}, {-96 * 16, 24 * 16}}
	freezeBiomes := []terrain.OverworldBiome{
		terrain.OverworldPlains, terrain.OverworldSnowyPlains, terrain.OverworldWindsweptHills,
		terrain.OverworldFrozenOcean, terrain.OverworldDeepFrozenOcean, terrain.OverworldJaggedPeaks,
	}
	rawYs := []int32{-40, 5, 30, 45, 61, 64}
	used := 0
	placedAny := false
	run := func(kind, name string, seed int64, x, y, z int32, cx, cz int32, place func(*decorRegion)) {
		clear(world.protos.mem)
		r := newDecorRegion(world, newChunk(cx, cz), worldMinY, worldHeight)
		r.nx, r.nz = cx, cz
		r.worldSeed = seed
		r.rng.SetSeed(seed)
		placedAny = false
		place(r)
		key := fmt.Sprintf("%s %s %d %d %d %d", kind, name, seed, x, y, z)
		if want, ok := results[key]; ok {
			if placedAny != want {
				t.Errorf("%s: placement success %v, vanilla %v", key, placedAny, want)
			}
			delete(results, key)
		}
		if want, ok := nextDraws[key]; ok {
			if got := r.rng.NextLong(); got != want {
				t.Errorf("%s: next random %d, vanilla %d", key, got, want)
			}
			delete(nextDraws, key)
		}
		if r.unorderedSet {
			// Equal full hashes in a tree bin require JVM identity hashes (blockPosSetOrder).
			if _, listed := expected[key]; listed {
				used++
			}
			return
		}
		sha, n := world.changes(r)
		w, listed := expected[key]
		if listed {
			used++
		} else if kind == "raw" {
			t.Errorf("%s: not in the golden", key)
			return
		} else {
			w = want{"0", ""}
		}
		if strconv.Itoa(n) != w.n || (n > 0 && sha != w.sha) {
			t.Errorf("%s: changed %d blocks %s, vanilla %s %s", key, n, sha, w.n, w.sha)
		}
	}
	for _, name := range order {
		large := name == "large_dripstone"
		if world.largeDripstone != large {
			clear(world.chunks)
		}
		world.largeDripstone = large
		var placed *overworldPlaced
		for i := range g.placed {
			if g.placed[i].feature.Name == "minecraft:"+name {
				placed = &g.placed[i]
			}
		}
		if strings.HasPrefix(name, "sculk_patch_") {
			mods, feature := compilePlaced(parseJSON(levelgen.PlacedFeatureJSON(name)), 0, true)
			placed = &overworldPlaced{mods: mods, place: func(r *decorRegion, p [3]int32) { placedAny = feature(r, p) || placedAny }}
		}
		if placed == nil {
			t.Errorf("%s is not placed", name)
			continue
		}
		// The golden's biome is one carrying the feature, so the biome filter,
		// last in the chain, always passes.
		mods := placed.mods
		if name != "sculk_patch_ancient_city" {
			mods = mods[:len(mods)-1]
		}
		cs, biomes := chunks, []terrain.OverworldBiome{terrain.OverworldPlains}
		ys := rawYs
		switch name {
		case "freeze_top_layer":
			cs, biomes = freezeChunks, freezeBiomes
		case "ice_spike", "ice_patch", "desert_well":
			cs = surfaceChunks
			ys = []int32{64, 69, 70, 71, 75, 100}
		}
		for i := 1; i <= counts[name]; i++ {
			seed := int64(uint64(i) * 0x9E3779B97F4A7C15)
			c := cs[i%len(cs)]
			world.biome = biomes[i%len(biomes)]
			run("case", name, seed, c[0], worldMinY, c[1], c[0]>>4, c[1]>>4, func(r *decorRegion) {
				r.walk(mods, [3]int32{c[0], worldMinY, c[1]}, placed.place)
			})
			if i > 12 || !hasRaw[name] {
				continue
			}
			for _, y := range ys {
				run("raw", name, seed, c[0]+8, y, c[1]+8, c[0]>>4, c[1]>>4, func(r *decorRegion) {
					placed.place(r, [3]int32{c[0] + 8, y, c[1] + 8})
				})
			}
		}
	}
	if len(results) != 0 {
		t.Errorf("%d placement-result records were not checked", len(results))
	}
	if len(nextDraws) != 0 {
		t.Errorf("%d random-draw records were not checked", len(nextDraws))
	}
	if used != len(expected) {
		t.Errorf("%d golden lines, %d matched", len(expected), used)
	}
}
