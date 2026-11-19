package world

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/golem-mc/golem/internal/server/world/chunksched"
	"github.com/golem-mc/golem/registry"
)

// netherGens holds one generator per seed, so the chunks a test makes share
// the shaped terrain their decorations read.
var netherGens sync.Map

// netherChunk generates a chunk of the seeded nether, the way a nether test
// wants it: directly, without a world around it.
func netherChunk(seed int64, x, z int32) *Chunk {
	g, _ := netherGens.LoadOrStore(seed, NewNetherGenerator(seed))
	c := newChunk(x, z)
	g.(*NetherGenerator).Generate(c)
	return c
}

// The nether generator is a Generator, which is what a world would hand
// NewWorld once dimensions are wired up.
var _ Generator = (*NetherGenerator)(nil)

// TestNetherIsDeterministic checks the contract the world's whole
// generate-and-throw-away scheme rests on: the same seed and coordinates
// produce the same chunk, from a fresh generator as much as from one that
// has generated others before, and at negative coordinates as much as at
// positive ones.
func TestNetherIsDeterministic(t *testing.T) {
	t.Parallel()
	g1 := NewNetherGenerator(42)
	g2 := NewNetherGenerator(42)

	for _, pos := range []ChunkPos{{3, -2}, {-1, -1}, {0, 0}, {17, 40}} {
		c1, c2 := newChunk(pos.X, pos.Z), newChunk(pos.X, pos.Z)
		g1.Generate(c1)
		g2.Generate(c2)

		if !bytes.Equal(encodeWhole(c1), encodeWhole(c2)) {
			t.Fatalf("chunk (%d, %d) differs between two generators of seed 42", pos.X, pos.Z)
		}
		// The block check walks the whole column of one spot per
		// section, both inside the nether's band and outside it.
		for y := int32(netherBottom); y < netherTop; y += 7 {
			if c1.BlockAt(5, y, 9) != c2.BlockAt(5, y, 9) {
				t.Fatalf("chunk (%d, %d) block at y=%d differs", pos.X, pos.Z, y)
			}
		}
	}
}

// TestNetherBedrockFloorAndCeiling checks the top and bottom of the nether's
// band: solid bedrock at y=0 under every column and at y=127 over every one,
// noisy layers above the floor and below the ceiling, no bedrock anywhere
// between the two bands, and nothing at all outside the 0..127 band the
// nether builds in — the chunk's own range keeps the rest as air.
func TestNetherBedrockFloorAndCeiling(t *testing.T) {
	t.Parallel()
	g := NewNetherGenerator(5)
	c := netherChunk(5, 0, 0)

	thickLow, thickHigh := false, false
	for x := range int32(16) {
		for z := range int32(16) {
			if got := c.BlockAt(x, 0, z); got != g.bedrockState {
				t.Errorf("block at (%d, 0, %d) = %d, want bedrock", x, z, got)
			}
			if got := c.BlockAt(x, 127, z); got != g.bedrockState {
				t.Errorf("block at (%d, 127, %d) = %d, want bedrock", x, z, got)
			}
			// The layers inside the bands are bedrock only where the
			// surface rule's vertical gradient draws it.
			for _, y := range []int32{1, 2, 3, 4} {
				if c.BlockAt(x, y, z) == g.bedrockState {
					thickLow = true
				}
			}
			for _, y := range []int32{123, 124, 125, 126} {
				if c.BlockAt(x, y, z) == g.bedrockState {
					thickHigh = true
				}
			}
			// Between the bands no bedrock at all: the floor is at
			// most five layers and the ceiling at most five.
			for y := int32(5); y <= 122; y++ {
				if got := c.BlockAt(x, y, z); got == g.bedrockState {
					t.Errorf("bedrock at y=%d, between the floor and ceiling bands", y)
				}
			}
			// Outside the nether's band, the air a chunk starts as.
			for _, y := range []int32{worldMinY, -1, 128, worldMinY + worldHeight - 1} {
				if got := c.BlockAt(x, y, z); got != airState {
					t.Errorf("block at (%d, %d, %d) = %d, want air outside the nether's band", x, y, z, got)
				}
			}
		}
	}
	if !thickLow {
		t.Error("the bedrock floor is one block thick everywhere, want the noisy layers of the 0..4 band")
	}
	if !thickHigh {
		t.Error("the bedrock ceiling is one block thick everywhere, want the noisy layers of the 123..126 band")
	}
}

// TestNetherLavaSea checks the lava at the bottom of the world: some column
// of the origin chunk has lava at y=31, the sea's surface, and no open cell
// at or below the lava line stayed air — every one of them is lava or rock.
func TestNetherLavaSea(t *testing.T) {
	t.Parallel()
	g := NewNetherGenerator(5)
	c := netherChunk(5, 0, 0)

	surface := false
	for x := range int32(16) {
		for z := range int32(16) {
			if c.BlockAt(x, netherLavaLevel, z) == g.lavaState {
				surface = true
			}
			for y := int32(netherBottom); y <= netherLavaLevel; y++ {
				if got := c.BlockAt(x, y, z); got == airState {
					t.Errorf("air at (%d, %d, %d), below the lava line: the sea left a hole", x, y, z)
				}
			}
		}
	}
	if !surface {
		t.Error("no lava at y=31 in the origin chunk, want the surface of the lava sea")
	}
}

// TestNetherGlowstoneAndOres rounds the features up in one sweep of sixteen
// chunks: glowstone hanging under the cavern ceilings, and quartz, nether
// gold and magma scattered through the netherrack — the magma in vanilla's
// band over the lava sea's shore.
// vanilla: ore_quartz_nether, ore_gold_nether, ore_magma
func TestNetherGlowstoneAndOres(t *testing.T) {
	t.Parallel()
	g := NewNetherGenerator(5)

	var glow, quartz, gold, magma int
	for cx := int32(0); cx <= 3; cx++ {
		for cz := int32(0); cz <= 3; cz++ {
			c := netherChunk(5, cx, cz)
			for x := range int32(16) {
				for z := range int32(16) {
					for y := range int32(netherTop) {
						switch c.BlockAt(x, y, z) {
						case g.glowstoneState:
							glow++
						case g.quartzOreState:
							quartz++
						case g.goldOreState:
							gold++
						case g.magmaState:
							magma++
						}
					}
				}
			}
		}
	}
	if glow < 5 {
		t.Errorf("only %d glowstone blocks in 16 chunks, want the clusters under the ceilings", glow)
	}
	if quartz < 20 {
		t.Errorf("only %d nether quartz ore blocks in 16 chunks", quartz)
	}
	if gold < 8 {
		t.Errorf("only %d nether gold ore blocks in 16 chunks", gold)
	}
	if magma < 100 {
		t.Errorf("only %d magma blocks in 16 chunks, want the four veins of 33 every chunk draws", magma)
	}
}

// TestNetherBiomes checks what the chunks carry: every 4x4x4 cell holds the
// noise biome of its quart column, the one the features' biome filters read,
// and a sweep of the biome source turns up all five biomes.
func TestNetherBiomes(t *testing.T) {
	t.Parallel()
	g := NewNetherGenerator(5)

	for _, pos := range []ChunkPos{{0, 0}, {-7, 3}} {
		c := netherChunk(5, pos.X, pos.Z)
		for x := int32(0); x < sectionSize; x += 4 {
			for z := int32(0); z < sectionSize; z += 4 {
				want := g.regionAt(pos.X*sectionSize+x, pos.Z*sectionSize+z)
				for y := int32(worldMinY); y < worldMinY+worldHeight; y += 4 {
					if got := c.BiomeAt(x, y, z); got != want {
						t.Fatalf("chunk %v cell (%d, %d, %d): biome %d, want %d", pos, x, y, z, got, want)
					}
				}
			}
		}
	}

	seen := map[int32]bool{}
	for x := int32(-4096); x <= 4096; x += 128 {
		for z := int32(-4096); z <= 4096; z += 128 {
			seen[g.regionAt(x, z)] = true
		}
	}
	for _, biome := range g.biomes {
		if !seen[biome] {
			t.Errorf("a sweep of the biome source missed biome %d", biome)
		}
	}
}

// BenchmarkNetherGenerate measures one chunk of the nether being made:
// caverns, bedrock bands, the lava sea, surfaces, glowstone, ores and biomes.
func BenchmarkNetherGenerate(b *testing.B) {
	g := NewNetherGenerator(5)
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		c := newChunk(int32(b.N)%64, int32(b.N)/64%64)
		g.Generate(c)
	}
}

// BenchmarkNetherBuildTerrain measures one chunk's noise fill, surfaces and
// carvers, copied into a fresh chunk.
func BenchmarkNetherBuildTerrain(b *testing.B) {
	g := NewNetherGenerator(5)
	g.buildTerrain(newChunk(100, 100), true) // warm
	b.ReportAllocs()
	i := int32(0)
	for b.Loop() {
		g.buildTerrain(newChunk(i%64, i/64), true)
		i++
	}
}

// TestNetherMatchesVanilla compares the Nether with vanilla's own chunks at
// each generation step; the feature checks read the same cached chunk ranges.
func TestNetherMatchesVanilla(t *testing.T) {
	t.Parallel()
	t.Run("NetherTerrain", testNetherTerrain)
	t.Run("NetherCarvers", testNetherCarvers)
	t.Run("NetherFeatures", testNetherFeatures)
	t.Run("NetherTwistingVines", testNetherTwistingVines)
}

// testNetherTerrain compares the terrain before the features — the noise fill,
// the lava sea, the biomes and the surface rule — with vanilla's own chunks,
// block for block, through testdata/nether_terrain.txt. The chunks cover all
// five biomes and the borders between them.
// vanilla: NoiseBasedChunkGenerator.fillFromNoise, buildSurface
func testNetherTerrain(t *testing.T) {
	t.Parallel()
	checkNetherDigests(t, "testdata/nether_terrain.txt", (*NetherGenerator).shapeTerrain)
}

// testNetherCarvers compares the terrain after the nether_cave carver with
// vanilla's own chunks, block for block, through testdata/nether_carved.txt:
// the caves, their lava below y 32, and the blocks they leave, at seeds and
// ranges near and far from the origin.
// vanilla: NoiseBasedChunkGenerator.generateCarvers, NetherWorldCarver
func testNetherCarvers(t *testing.T) {
	t.Parallel()
	checkNetherDigests(t, "testdata/nether_carved.txt", func(g *NetherGenerator, c *Chunk) {
		g.buildTerrain(c, true)
	})
}

// checkNetherDigests builds every chunk a digest file lists and compares the
// SHA-256 of its blocks in y 0..127, each as "name[properties]\n" in y, z, x
// order, with the file's.
func checkNetherDigests(t *testing.T, path string, build func(*NetherGenerator, *Chunk)) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	gens := map[int64]*NetherGenerator{}
	checked := 0
	for line := range strings.SplitSeq(string(data), "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		var seed int64
		var cx, cz int32
		var want string
		if _, err := fmt.Sscan(line, &seed, &cx, &cz, &want); err != nil {
			t.Fatalf("%q: %v", line, err)
		}
		g := gens[seed]
		if g == nil {
			g = NewNetherGenerator(seed)
			// The vanilla chunks were made with generate-structures=false.
			g.DisableStructures()
			gens[seed] = g
		}
		c := newChunk(cx, cz)
		build(g, c)
		if got := chunkBlockDigest(c, 0, netherTop); got != want {
			t.Errorf("seed %d chunk (%d, %d): digest %s, want vanilla's %s", seed, cx, cz, got, want)
		}
		checked++
	}
	if checked == 0 {
		t.Fatal("no chunks compared")
	}
}

// netherDigestRange is a seed and an inclusive chunk range.
type netherDigestRange struct {
	seed           int64
	x0, z0, x1, z1 int32
}

// netherDigestRanges are the chunk ranges testdata/nether_features.txt was
// read from: vanilla generated each by forceloading it. The file lists only the
// chunks every run gave the same digest, so they do not depend on the order its
// scheduler decorated the chunks in.
var netherDigestRanges = []netherDigestRange{
	{0, -4, -4, 3, 3},
	{12345, -4, -4, 3, 3},
	{0, 40, -30, 47, -23},
	{0, -42, -16, -35, -9},
	{0, 32, 8, 39, 15},
}

// generateNetherRange generates a range of chunks with a fresh generator in
// the order vanilla's chunk scheduler runs the FEATURES step while /forceload
// adds the range; the chunks around the range come with it, and are dropped.
// Block/structure goldens share copies; tick/order tests use the uncached With.
func generateNetherRange(seed int64, x0, z0, x1, z1 int32) map[ChunkPos]*Chunk {
	return cachedNetherRange(seed, x0, z0, x1, z1)
}

// generateNetherRangeWith is generateNetherRange that also returns the
// generator.
func generateNetherRangeWith(seed int64, x0, z0, x1, z1 int32) (*NetherGenerator, map[ChunkPos]*Chunk) {
	g := NewNetherGenerator(seed)
	out := map[ChunkPos]*Chunk{}
	generate := func(cx, cz int32) {
		if _, done := out[ChunkPos{X: cx, Z: cz}]; done {
			return
		}
		c := newChunk(cx, cz)
		g.Generate(c)
		if cx >= x0 && cx <= x1 && cz >= z0 && cz <= z1 {
			out[ChunkPos{X: cx, Z: cz}] = c
		}
	}
	for _, p := range chunksched.Forceload([]chunksched.Rect{{X0: x0, Z0: z0, X1: x1, Z1: z1}}) {
		generate(p.X, p.Z)
	}
	for cx := x0; cx <= x1; cx++ {
		for cz := z0; cz <= z1; cz++ {
			generate(cx, cz)
		}
	}
	return g, out
}

// checkNetherFeatureDigests generates each of the ranges and compares the
// SHA-256 of the blocks in y 0..127 of every listed chunk against vanilla.
// A recorded-order replay distinguishes block errors from the scheduler model.
func checkNetherFeatureDigests(t *testing.T, path string, ranges []netherDigestRange) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	type key struct {
		seed int64
		pos  ChunkPos
	}
	want := map[key]string{}
	for line := range strings.SplitSeq(string(data), "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		var k key
		var sum string
		if _, err := fmt.Sscan(line, &k.seed, &k.pos.X, &k.pos.Z, &sum); err != nil {
			t.Fatalf("%q: %v", line, err)
		}
		if _, dup := want[k]; dup {
			t.Fatalf("%q: a second digest for the chunk", line)
		}
		want[k] = sum
	}
	// A file may list more ranges than the test runs; only the chunks of the
	// ranges it runs are compared.
	wanted := 0
	for k := range want {
		for _, r := range ranges {
			if k.seed == r.seed && k.pos.X >= r.x0 && k.pos.X <= r.x1 && k.pos.Z >= r.z0 && k.pos.Z <= r.z1 {
				wanted++
				break
			}
		}
	}
	var mu sync.Mutex
	checked := 0
	// The group's parallel ranges all finish before t.Run returns.
	t.Run("ranges", func(t *testing.T) {
		for _, r := range ranges {
			t.Run(fmt.Sprintf("%d_%d_%d_%d_%d", r.seed, r.x0, r.z0, r.x1, r.z1), func(t *testing.T) {
				t.Parallel()
				chunks := generateNetherRange(r.seed, r.x0, r.z0, r.x1, r.z1)
				var recorded map[ChunkPos]*Chunk
				n := 0
				for pos, c := range chunks {
					sum, ok := want[key{r.seed, pos}]
					if !ok {
						continue
					}
					if got := chunkBlockDigest(c, 0, netherTop); got != sum {
						if recorded == nil {
							recorded = recordedNetherRange(t, r)
						}
						if replay := chunkBlockDigest(recorded[pos], 0, netherTop); replay != sum {
							t.Errorf("seed %d chunk (%d, %d): modeled digest %s, replay %s, want vanilla's %s", r.seed, pos.X, pos.Z, got, replay, sum)
						} else {
							t.Logf("seed %d chunk (%d, %d): recorded order matches vanilla; approximate scheduler differs", r.seed, pos.X, pos.Z)
						}
					}
					n++
				}
				mu.Lock()
				checked += n
				mu.Unlock()
			})
		}
	})
	if checked != wanted || checked == 0 {
		t.Fatalf("compared %d chunks, the file lists %d in the ranges", checked, wanted)
	}
}

// testNetherFeatures compares the whole Nether with vanilla's own
// chunks, block for block, through testdata/nether_features.txt: the springs,
// fire, glowstone, mushrooms, ores, ancient debris, blobs, the delta, the
// basalt columns and pillars, the fungi, the forests' roots, sprouts and vines,
// placed in decoration order with each feature's own seed, across the chunks
// around them.
// vanilla: ChunkGenerator.applyBiomeDecoration
func testNetherFeatures(t *testing.T) {
	t.Parallel()
	checkNetherFeatureDigests(t, "testdata/nether_features.txt", netherDigestRanges)
}

// testNetherTwistingVines compares the twisting vines of a warped
// forest chunk with vanilla's: SHA-256 of "x y z name[properties]\n" for each
// vine block in y, z, x order, read back from the reference server. The chunk's
// vines are compared independently of the other vegetation.
// vanilla: TwistingVinesFeature.place
func testNetherTwistingVines(t *testing.T) {
	t.Parallel()
	const want = "a2d2ed28533965b7ae339617ada72bfd7e5a82a372b7a6875aedb890dc18997e"
	c := generateNetherRange(0, 32, 8, 39, 15)[ChunkPos{X: 37, Z: 12}]
	h := sha256.New()
	for y := range int32(netherTop) {
		for z := range int32(sectionSize) {
			for x := range int32(sectionSize) {
				if name, props, _ := registry.BlockStateName(c.BlockAt(x, y, z)); strings.HasPrefix(name, "minecraft:twisting_vines") {
					fmt.Fprintf(h, "%d %d %d %s[%s]\n", x, y, z, name, props)
				}
			}
		}
	}
	if got := hex.EncodeToString(h.Sum(nil)); got != want {
		t.Errorf("twisting vines digest %s, want vanilla's %s", got, want)
	}
}

// BenchmarkNetherGenerateRun measures an 8x8 run of neighbouring chunks, the
// shape of a player walking over new ground.
func BenchmarkNetherGenerateRun(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		g := NewNetherGenerator(5)
		for cx := range int32(8) {
			for cz := range int32(8) {
				g.Generate(newChunk(cx, cz))
			}
		}
	}
}

// BenchmarkNetherGenerateCold measures one chunk with nothing around it
// generated yet.
func BenchmarkNetherGenerateCold(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		g := NewNetherGenerator(5)
		g.Generate(newChunk(100, 100))
	}
}
