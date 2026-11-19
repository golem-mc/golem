package world

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/golem-mc/golem/registry"
)

// The golden lines in levelgen/testdata/overworld_trees_golden.txt are what
// the vanilla server's own tree features write on a synthetic world
// (tools/java/OverworldTreeGolden.java, mise run gen:overworld-trees-golden).
// The world is treeGoldenSurface's columns, which the Java side computes the same
// way. Its deep lines run on the same world with every floor
// treeGoldenDeepDrop blocks lower, deep enough for a full kelp column.

const (
	treeGoldenSeaTop   = 62
	treeGoldenDeepDrop = 30
)

// treeGoldenSurface is the top solid block of a column: 8x8 cells of one height.
func treeGoldenSurface(x, z int32) int32 {
	v := (x>>3)*73856093 ^ (z>>3)*19349663
	return 58 + int32(uint32(v)>>8%9)
}

var treeGoldenStates = struct{ grass, dirt, stone, sand, water, bedrock uint16 }{
	mustState("minecraft:grass_block", nil), mustState("minecraft:dirt", nil),
	mustState("minecraft:stone", nil), mustState("minecraft:sand", nil),
	mustState("minecraft:water", nil), mustState("minecraft:bedrock", nil),
}

// treeGoldenBase is the world's block with every floor drop blocks lower.
func treeGoldenBase(drop, x, y, z int32) uint16 {
	if y < worldMinY || y >= worldMinY+worldHeight {
		return airState
	}
	s := treeGoldenStates
	if y == worldMinY {
		return s.bedrock
	}
	h := treeGoldenSurface(x, z) - drop
	switch {
	case y > h:
		if y <= treeGoldenSeaTop {
			return s.water
		}
		return airState
	case y == h:
		if h <= treeGoldenSeaTop {
			return s.sand
		}
		return s.grass
	case y >= h-3:
		if h <= treeGoldenSeaTop {
			return s.sand
		}
		return s.dirt
	}
	if y >= h-12 && (x>>3+z>>3)&1 == 0 {
		return airState // a cave under every other cell
	}
	return s.stone
}

// treeGoldenTerrain is a terrainSource over treeGoldenBase; each case gets a fresh
// decorState and shares the shaped chunks.
type treeGoldenTerrain struct {
	drop   int32
	mu     sync.Mutex
	shaped map[ChunkPos]*shapedTerrain
	ds     decorState
}

func (g *treeGoldenTerrain) cachedTerrain(cx, cz int32) *shapedTerrain {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.shaped[ChunkPos{X: cx, Z: cz}]
}

func (g *treeGoldenTerrain) terrainOf(cx, cz int32, _ *Chunk) *shapedTerrain {
	pos := ChunkPos{X: cx, Z: cz}
	g.mu.Lock()
	defer g.mu.Unlock()
	if t := g.shaped[pos]; t != nil {
		return t
	}
	t := &shapedTerrain{}
	for sec := range int32(sectionCount) {
		b := newBlockWords(airState)
		for y := range int32(sectionSize) {
			for z := range int32(sectionSize) {
				for x := range int32(sectionSize) {
					b = b.put(blockIndex(x, y, z), treeGoldenBase(g.drop, cx*16+x, worldMinY+sec*16+y, cz*16+z), false)
				}
			}
		}
		t.blocks[sec] = b
	}
	g.shaped[pos] = t
	return t
}

func (g *treeGoldenTerrain) decor() *decorState  { return &g.ds }
func (g *treeGoldenTerrain) isAir(s uint16) bool { return s == airState }
func (g *treeGoldenTerrain) biomeIndex(*decorRegion, int32, int32, int32) uint {
	return 0
}

// treeGoldenRun runs place on a region centred on chunk (cx, cz) with the feature
// seed of slot (index, step), and returns the digest line tail: "<changes>
// <sha256>".
func treeGoldenRun(src *treeGoldenTerrain, seed int64, cx, cz, index, step int32, place func(*decorRegion) bool) (placed bool, digest string, unordered bool) {
	src.ds = decorState{}
	r := newDecorRegion(src, newChunk(cx, cz), worldMinY, worldHeight)
	r.nx, r.nz = cx, cz
	r.worldSeed = seed
	dec := r.rng.SetDecorationSeed(seed, cx*16, cz*16)
	r.rng.SetFeatureSeed(dec, index, step)
	placed = place(r)
	return placed, treeGoldenDigest(r, src.drop), r.unorderedSet
}

func treeGoldenDigest(r *decorRegion, drop int32) string {
	type change struct {
		x, y, z int32
		state   uint16
	}
	type hive struct {
		x, y, z int32
		ticks   []int64
	}
	var changes []change
	var hives []hive
	for i, p := range r.protos {
		if p == nil {
			continue
		}
		cx, cz := r.center.x+int32(i%decorRegionSpan)-decorRegionSpan/2, r.center.z+int32(i/decorRegionSpan)-decorRegionSpan/2
		for sec, blocks := range p.blocks {
			if blocks == nil {
				continue
			}
			for y := range int32(sectionSize) {
				for z := range int32(sectionSize) {
					for x := range int32(sectionSize) {
						v := blocks[protoIndex(x, y, z)]
						wx, wy, wz := cx*16+x, worldMinY+int32(sec)*16+y, cz*16+z
						if v != 0 && v-1 != treeGoldenBase(drop, wx, wy, wz) {
							changes = append(changes, change{wx, wy, wz, v - 1})
						}
					}
				}
			}
		}
		for _, e := range p.entities {
			if !e.hive {
				continue
			}
			h := hive{x: cx*16 + e.x, y: e.y + worldMinY, z: cz*16 + e.z}
			for b := range e.lootSeed >> 30 {
				h.ticks = append(h.ticks, e.lootSeed>>(10*b)&1023)
			}
			hives = append(hives, h)
		}
	}
	sort.Slice(changes, func(i, j int) bool {
		a, b := changes[i], changes[j]
		if a.x != b.x {
			return a.x < b.x
		}
		if a.y != b.y {
			return a.y < b.y
		}
		return a.z < b.z
	})
	sort.Slice(hives, func(i, j int) bool {
		a, b := hives[i], hives[j]
		if a.x != b.x {
			return a.x < b.x
		}
		if a.y != b.y {
			return a.y < b.y
		}
		return a.z < b.z
	})
	sum := sha256.New()
	for _, c := range changes {
		fmt.Fprintf(sum, "%d %d %d %s\n", c.x, c.y, c.z, treeGoldenStateName(c.state))
	}
	for _, h := range hives {
		fmt.Fprintf(sum, "hive %d %d %d", h.x, h.y, h.z)
		for _, t := range h.ticks {
			fmt.Fprintf(sum, " %d", t)
		}
		fmt.Fprintln(sum)
	}
	return fmt.Sprintf("%d %s", len(changes), hex.EncodeToString(sum.Sum(nil)))
}

func treeGoldenStateName(s uint16) string {
	name, props, _ := registry.StateNameProps(s)
	if len(props) == 0 {
		return name
	}
	keys := make([]string, 0, len(props))
	for k := range props {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for i, k := range keys {
		keys[i] = k + "=" + props[k]
	}
	return name + "[" + strings.Join(keys, ",") + "]"
}

func TestOverworldTreesGolden(t *testing.T) {
	t.Parallel()
	f, err := os.Open("levelgen/testdata/overworld_trees_golden.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	src := &treeGoldenTerrain{shaped: map[ChunkPos]*shapedTerrain{}}
	deep := &treeGoldenTerrain{drop: treeGoldenDeepDrop, shaped: map[ChunkPos]*shapedTerrain{}}
	// Deep lines run the generator's own placed features, so a change to the
	// data generation uses shows here and not only in overworld_trees.txt.
	var gen *OverworldGenerator
	var checked, placedCases, skipped, deepPlaced int
	kinds := map[string]bool{}
	sc := bufio.NewScanner(f)
	for line := 1; sc.Scan(); line++ {
		fs := strings.Fields(sc.Text())
		if len(fs) == 0 || fs[0] == "#" {
			continue
		}
		n := func(i int) int64 {
			v, err := strconv.ParseInt(fs[i], 10, 64)
			if err != nil {
				t.Fatalf("line %d: %v", line, err)
			}
			return v
		}
		name, seed, cx, cz, index, step := fs[1], n(2), int32(n(3)), int32(n(4)), int32(n(5)), int32(n(6))
		var run func(*decorRegion) bool
		var want string
		var wantPlaced bool
		on := src
		switch fs[0] {
		case "deep":
			if gen == nil {
				gen = NewOverworldGenerator(0)
				gen.initOverworldFeatures()
			}
			i := slices.IndexFunc(gen.placed, func(p overworldPlaced) bool { return p.feature.Name == name })
			if i < 0 {
				t.Errorf("line %d: %s is not placed in generation", line, name)
				continue
			}
			pf := gen.placed[i]
			// The biome filter, last in the chain, is the decoration pipeline's.
			mods := pf.mods[:len(pf.mods)-1]
			run = func(r *decorRegion) bool {
				placed := false
				r.walk(mods, [3]int32{cx * 16, worldMinY, cz * 16}, func(r *decorRegion, p [3]int32) {
					placed = true
					pf.place(r, p)
				})
				return placed
			}
			on = deep
			wantPlaced, want = fs[7] == "1", fs[8]+" "+fs[9]
		case "placed":
			pf, ok := treeFeature(name, 0)
			if !ok {
				skipped++
				continue
			}
			run = func(r *decorRegion) bool { return pf.run(r, treePos{cx * 16, worldMinY, cz * 16}) }
			wantPlaced, want = fs[7] == "1", fs[8]+" "+fs[9]
		case "configured":
			cf := compileConfigured(name)
			x, y, z := int32(n(7)), int32(n(8)), int32(n(9))
			run = func(r *decorRegion) bool { return cf(r, treePos{x, y, z}) }
			wantPlaced, want = fs[10] == "1", fs[11]+" "+fs[12]
		default:
			t.Fatalf("line %d: %q", line, fs[0])
		}
		placed, got, unordered := treeGoldenRun(on, seed, cx, cz, index, step, run)
		if unordered { // equal full hashes in a tree bin require JVM identity hashes
			skipped++
			continue
		}
		checked++
		if wantPlaced && fs[0] == "deep" {
			deepPlaced++
		}
		if wantPlaced {
			placedCases++
			kinds[name] = true
		}
		if placed != wantPlaced || got != want {
			t.Errorf("%s", strings.Join(fs[:min(len(fs), 11)], " ")+fmt.Sprintf(": placed %v changes/sha %s, vanilla placed %v %s", placed, got, wantPlaced, want))
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	t.Logf("%d cases checked, %d placed something across %d features, %d skipped (unported)", checked, placedCases, len(kinds), skipped)
	if deepPlaced < 16 {
		t.Fatalf("the deep-ocean golden exercises too little: %d placing cases", deepPlaced)
	}
	if placedCases < 300 || len(kinds) < 40 {
		t.Fatalf("the golden exercises too little: %d placing cases over %d features", placedCases, len(kinds))
	}
}
