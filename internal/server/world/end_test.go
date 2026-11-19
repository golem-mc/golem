package world

import (
	"bytes"
	"fmt"
	"hash/fnv"
	"maps"
	"testing"

	"github.com/golem-mc/golem/registry"
)

// endChunk generates a chunk of the seeded end, the way an end test wants it:
// directly, without a world around it.
func endChunk(seed int64, x, z int32) *Chunk {
	g := NewEndGenerator(seed)
	c := newChunk(x, z)
	g.Generate(c)
	return c
}

// biomeAt reports the biome the chunk whose centre column is at (x, z) would
// carry, which only the end tests need.
func (g *EndGenerator) biomeAt(x, z int32) int32 {
	return g.biomeForChunk(floorDiv(x, sectionSize), floorDiv(z, sectionSize))
}

// The main island's top near the origin stays within islandTopMin..islandTopMax
// for every seed: 57..65 over seeds 0..29.
const islandTopMin, islandTopMax = 55, 67

// The end generator is a Generator, which is what a world would hand NewWorld
// once dimensions are wired up.
var _ Generator = (*EndGenerator)(nil)

// TestEndIsDeterministic checks the contract the world's whole
// generate-and-throw-away scheme rests on: the same seed and coordinates
// produce the same chunk, from a fresh generator as much as from one that
// has generated others before, and at negative coordinates as much as at
// positive ones.
func TestEndIsDeterministic(t *testing.T) {
	t.Parallel()
	g1 := NewEndGenerator(42)
	g2 := NewEndGenerator(42)

	for _, pos := range []ChunkPos{{3, -2}, {-1, -1}, {0, 0}, {17, 40}, {40, 0}} {
		c1, c2 := newChunk(pos.X, pos.Z), newChunk(pos.X, pos.Z)
		g1.Generate(c1)
		g2.Generate(c2)

		if !bytes.Equal(encodeWhole(c1), encodeWhole(c2)) {
			t.Fatalf("chunk (%d, %d) differs between two generators of seed 42", pos.X, pos.Z)
		}
		for y := int32(netherBottom); y < netherTop; y += 7 {
			if c1.BlockAt(5, y, 9) != c2.BlockAt(5, y, 9) {
				t.Fatalf("chunk (%d, %d) block at y=%d differs", pos.X, pos.Z, y)
			}
		}
	}
}

// TestEndMainIsland checks the island under the origin chunk: its centre
// column rises to a surface of end stone within the 55..67 band, carries end
// stone some way below that surface rather than being a skin, and has open
// void above it. Six hundred blocks east there is nothing but air the whole
// way down: the void between the islands.
func TestEndMainIsland(t *testing.T) {
	t.Parallel()
	g := NewEndGenerator(5)
	c := endChunk(5, 0, 0)

	top := int32(-1)
	for y := int32(worldMinY + worldHeight - 1); y >= worldMinY; y-- {
		if c.BlockAt(8, y, 8) != airState {
			top = y
			break
		}
	}
	if top < islandTopMin || top > islandTopMax {
		t.Fatalf("the island's surface at (8, 8) is at y=%d, outside the 55..67 band", top)
	}
	if got := c.BlockAt(8, top, 8); got != g.endStoneState {
		t.Errorf("the island's surface block = %d, want end stone", got)
	}
	for y := top - 5; y < top; y++ {
		if got := c.BlockAt(8, y, 8); got != g.endStoneState {
			t.Fatalf("block under the island's surface at y=%d = %d, want end stone", y, got)
		}
	}

	// The void, six hundred blocks east.
	far := endChunk(5, 600/16, 0)
	for x := range int32(16) {
		for z := range int32(16) {
			for y := int32(worldMinY); y < worldMinY+worldHeight; y++ {
				if got := far.BlockAt(x, y, z); got != airState {
					t.Fatalf("block at (%d, %d, %d) = %d, want the void 600 blocks out", 600+x, y, z, got)
				}
			}
		}
	}
}

// TestEndPillars checks the ring of obsidian pillars: a sweep of the chunks
// the ring stands in holds plenty of obsidian standing in many columns, and
// the obsidian stops well before the island's edge — no pillar out in the
// void where nothing built it.
func TestEndPillars(t *testing.T) {
	t.Parallel()
	g := NewEndGenerator(5)

	columns, blocks := 0, 0
	for cx := int32(-3); cx <= 3; cx++ {
		for cz := int32(-3); cz <= 3; cz++ {
			c := endChunk(5, cx, cz)
			for x := range int32(16) {
				for z := range int32(16) {
					inColumn := false
					for y := int32(endMinY); y <= 103; y++ {
						if c.BlockAt(x, y, z) == g.obsidianState {
							blocks++
							inColumn = true
						}
					}
					if inColumn {
						columns++
					}
				}
			}
		}
	}
	// Ten pillars of two to five blocks of radius stand on at least a
	// hundred columns between them.
	if columns < 100 {
		t.Errorf("obsidian stands in only %d columns across the pillar ring, want the ten pillars", columns)
	}
	if blocks < 1000 {
		t.Errorf("only %d obsidian blocks across the pillar ring, want the ten pillars", blocks)
	}

	// Well outside the ring, at (8, 8) of chunk (8, 8) — some 180 blocks
	// from the origin — nothing built anything.
	edge := endChunk(5, 8, 8)
	for x := range int32(16) {
		for z := range int32(16) {
			for y := int32(endMinY); y <= 103; y++ {
				if got := edge.BlockAt(x, y, z); got == g.obsidianState {
					t.Fatalf("obsidian at (%d, %d, %d), far outside the pillar ring", 8*16+x, y, 8*16+z)
				}
			}
		}
	}
}

// TestEndArrivalPlatform checks the platform players arrive on: obsidian five
// by five at y=48 centred on (100, 0), three blocks of air cleared above it,
// baked into the chunks it straddles and matching what EndArrivalPlatform
// hands out to place by hand.
func TestEndArrivalPlatform(t *testing.T) {
	t.Parallel()
	chunks := map[ChunkPos]*Chunk{
		{X: floorDiv(endArrivalX-2, sectionSize), Z: floorDiv(endArrivalZ-2, sectionSize)}: endChunk(5, floorDiv(endArrivalX-2, sectionSize), floorDiv(endArrivalZ-2, sectionSize)),
		{X: floorDiv(endArrivalX-2, sectionSize), Z: floorDiv(endArrivalZ+2, sectionSize)}: endChunk(5, floorDiv(endArrivalX-2, sectionSize), floorDiv(endArrivalZ+2, sectionSize)),
	}
	for _, change := range EndArrivalPlatform() {
		pos := ChunkPos{X: floorDiv(change.X, sectionSize), Z: floorDiv(change.Z, sectionSize)}
		c, ok := chunks[pos]
		if !ok {
			t.Fatalf("the platform reaches into chunk (%d, %d), which the test did not generate", pos.X, pos.Z)
		}
		lx, lz := change.X-pos.X*sectionSize, change.Z-pos.Z*sectionSize
		if got := c.BlockAt(lx, change.Y, lz); got != change.State {
			t.Errorf("platform block at (%d, %d, %d) = %d, want the change's state %d",
				change.X, change.Y, change.Z, got, change.State)
		}
	}
	if got := EndArrival(); got != (SpawnPos{X: endArrivalX, Y: endArrivalY + 1, Z: endArrivalZ}) {
		t.Errorf("EndArrival() = %+v, want standing on the platform", got)
	}
	if len(EndArrivalPlatform()) != 100 {
		t.Errorf("EndArrivalPlatform() returned %d changes, want 25 obsidian and 75 air", len(EndArrivalPlatform()))
	}
}

// TestEndBiomes checks what the chunks carry and where: the origin chunk sits
// wholly in the_end, out past the centre the chunk takes one of the four outer
// biomes, and every one of them turns up in a sweep of the ring.
func TestEndBiomes(t *testing.T) {
	t.Parallel()
	g := NewEndGenerator(5)

	c := endChunk(5, 0, 0)
	for y := int32(netherBottom); y < netherTop; y += sectionSize {
		if biome := c.sections[(y-worldMinY)/sectionSize].biome; biome != g.theEnd {
			t.Fatalf("the origin chunk's biome at y=%d = %d, want the_end", y, biome)
		}
	}
	if got := g.biomeAt(0, 0); got != g.theEnd {
		t.Errorf("biomeAt(0, 0) = %d, want the_end", got)
	}

	// Out past the centre's disc: one of the four outer biomes.
	farBiome := g.biomeAt(2000, 0)
	if farBiome != g.endHighlands && farBiome != g.endMidlands &&
		farBiome != g.smallEndIslands && farBiome != g.endBarrens {
		t.Errorf("biomeAt(2000, 0) = %d, want one of the four outer biomes", farBiome)
	}

	// The outer biomes all exist somewhere within a few thousand blocks.
	seen := map[int32]bool{}
	for x := int32(-4000); x <= 4000; x += 64 {
		for z := int32(-4000); z <= 4000; z += 64 {
			seen[g.biomeAt(x, z)] = true
		}
	}
	for _, want := range []int32{g.endHighlands, g.endMidlands, g.smallEndIslands, g.endBarrens} {
		if !seen[want] {
			t.Errorf("biome %d nowhere in the sweep, want every outer biome", want)
		}
	}
}

// TestEndMatchesVanilla compares the End with vanilla's: the pillars' deal,
// the island terrain and the features.
func TestEndMatchesVanilla(t *testing.T) {
	t.Parallel()
	t.Run("EndPillarsTakeVanillasDeal", testEndPillarsTakeVanillasDeal)
	t.Run("EndTerrainMatchesTerrainGolden", testEndTerrainMatchesTerrainGolden)
	t.Run("EndFeatures", testEndFeatures)
}

// testEndPillarsTakeVanillasDeal checks the per-seed deal of sizes to the
// ring's ten spots: vanilla shuffles 0..9 onto positions the seed does not
// move, and each size hands its pillar radius 2+size/3 and top 76+3·size.
// The wants are what a replica of vanilla's spike cache drew for these
// seeds, checked against the reference sources.
// vanilla: EndSpikeFeature.getSpikesForLevel, EndSpikeFeature.SpikeCacheLoader.load
func testEndPillarsTakeVanillasDeal(t *testing.T) {
	t.Parallel()

	spot := [endPillarCount][2]int32{
		{42, 0}, {33, 24}, {12, 39}, {-13, 39}, {-34, 24},
		{-42, -1}, {-34, -25}, {-13, -40}, {12, -40}, {33, -25},
	}
	for _, tc := range []struct {
		seed   int64
		radius [endPillarCount]int32
		top    [endPillarCount]int32
	}{
		{seed: 1,
			radius: [endPillarCount]int32{3, 4, 2, 2, 4, 3, 5, 3, 4, 2},
			top:    [endPillarCount]int32{85, 94, 76, 82, 100, 91, 103, 88, 97, 79}},
		{seed: 2,
			radius: [endPillarCount]int32{2, 2, 3, 4, 2, 3, 3, 5, 4, 4},
			top:    [endPillarCount]int32{79, 76, 91, 94, 82, 88, 85, 103, 97, 100}},
	} {
		g := NewEndGenerator(tc.seed)
		for i, p := range g.pillars {
			if p.x != spot[i][0] || p.z != spot[i][1] {
				t.Errorf("seed %d: pillar %d stands at (%d, %d), want the ring spot (%d, %d)",
					tc.seed, i, p.x, p.z, spot[i][0], spot[i][1])
			}
			if p.radius != tc.radius[i] || p.top != tc.top[i] {
				t.Errorf("seed %d: pillar %d at (%d, %d) = radius %d top %d, want radius %d top %d",
					tc.seed, i, p.x, p.z, p.radius, p.top, tc.radius[i], tc.top[i])
			}
		}
	}
}

// BenchmarkEndGenerate measures one chunk of the end being made: the island's
// columns, the pillars clipped to the chunk, the arrival platform, biomes.
func BenchmarkEndGenerate(b *testing.B) {
	g := NewEndGenerator(5)
	b.ReportAllocs()
	b.ResetTimer()
	i := 0
	for b.Loop() {
		g.Generate(newChunk(int32(i%64), int32(i/64%64)))
		i++
	}
}

// BenchmarkEndGenerateOuter measures chunks on the outer islands, where the
// biome comes from the island noise.
func BenchmarkEndGenerateOuter(b *testing.B) {
	g := NewEndGenerator(5)
	b.ReportAllocs()
	b.ResetTimer()
	i := 0
	for b.Loop() {
		g.Generate(newChunk(int32(60+i%64), int32(60+i/64)))
		i++
	}
}

// checkEndPodium checks EndPodiumFeature's inactive pattern around (0, oy, 0)
// through at, which reads a world block.
func checkEndPodium(t *testing.T, g *EndGenerator, at func(x, y, z int32) uint16) {
	t.Helper()
	// The origin y is the top solid block at (0, 0), which is the bedrock column's top.
	top := int32(-1)
	for y := int32(worldMinY + worldHeight - 1); y >= worldMinY; y-- {
		if at(0, y, 0) != airState {
			top = y
			break
		}
	}
	if top < 0 || at(0, top, 0) != g.bedrockState {
		t.Fatalf("top block at (0, 0) is %d at y=%d, want bedrock", at(0, top, 0), top)
	}
	oy := top - 3
	if oy < islandTopMin || oy > islandTopMax {
		t.Fatalf("podium origin y=%d is off the island's surface band", oy)
	}
	for dx := int32(-4); dx <= 4; dx++ {
		for dz := int32(-4); dz <= 4; dz++ {
			d2 := dx*dx + dz*dz
			inside := float64(d2) < 6.25
			if !inside && float64(d2) >= 12.25 {
				continue
			}
			if got := at(dx, oy-1, dz); got != map[bool]uint16{true: g.bedrockState, false: g.endStoneState}[inside] {
				t.Errorf("block below the origin at (%d, %d) = %d", dx, dz, got)
			}
			want := uint16(airState)
			if !inside {
				want = g.bedrockState
			}
			if dx == 0 && dz == 0 {
				want = g.bedrockState
			}
			if got := at(dx, oy, dz); got != want {
				t.Errorf("origin layer at (%d, %d) = %d, want %d", dx, dz, got, want)
			}
		}
	}
	for y := range int32(4) {
		if at(0, oy+y, 0) != g.bedrockState {
			t.Errorf("bedrock column missing at y=%d", oy+y)
		}
	}
	for _, f := range [...]struct {
		dx, dz int32
		facing string
	}{{0, -1, "north"}, {0, 1, "south"}, {-1, 0, "west"}, {1, 0, "east"}} {
		want, _ := registry.FindState("minecraft:wall_torch", map[string]string{"facing": f.facing})
		if got := at(f.dx, oy+2, f.dz); got != want {
			t.Errorf("torch facing %s = %d, want %d", f.facing, got, want)
		}
	}
}

// vanilla: EnderDragonFight.exitPortalPattern
func TestEndPodiumMatches(t *testing.T) {
	for _, tc := range []struct {
		name    string
		missing [3]int32
		want    bool
	}{
		{"complete", [3]int32{4, 64, 0}, true},
		{"torch", [3]int32{1, 66, 0}, true},
		{"column", [3]int32{0, 67, 0}, false},
		{"rim", [3]int32{3, 64, 0}, false},
		{"floor", [3]int32{2, 63, 1}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			blocks := map[[3]int32]uint16{}
			PlaceEndPodium(0, 64, 0, true, func(x, y, z int32, state uint16) { blocks[[3]int32{x, y, z}] = state })
			delete(blocks, tc.missing)
			got := EndPodiumMatches(0, 64, 0, func(x, y, z int32) uint16 { return blocks[[3]int32{x, y, z}] })
			if got != tc.want {
				t.Errorf("matches = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestEndHighlandsGrowChorus checks that a sweep of highlands chunks stands
// chorus plants on end stone under chorus flowers with their connections
// set, and that chunks with no highlands around them grow none.
func TestEndHighlandsGrowChorus(t *testing.T) {
	t.Parallel()
	g := NewEndGenerator(5)
	plants, flowers := 0, 0
	for cz := int32(-400); cz < -360; cz++ {
		for cx := int32(-400); cx < -360; cx++ {
			c := newChunk(cx, cz)
			g.Generate(c)
			// A chorus tree may reach in from a highlands neighbour.
			highlands := false
			for dz := int32(-1); dz <= 1; dz++ {
				for dx := int32(-1); dx <= 1; dx++ {
					highlands = highlands || g.biomeForChunk(cx+dx, cz+dz) == g.endHighlands
				}
			}
			for x := range int32(sectionSize) {
				for z := range int32(sectionSize) {
					for y := int32(worldMinY); y < worldMinY+worldHeight; y++ {
						name := registryStateName(c.BlockAt(x, y, z))
						if name != "minecraft:chorus_plant" && name != "minecraft:chorus_flower" {
							continue
						}
						if !highlands {
							t.Fatalf("chunk (%d, %d): chorus outside the highlands", cx, cz)
						}
						if name == "minecraft:chorus_flower" {
							flowers++
							continue
						}
						plants++
						_, raw, _ := registry.BlockStateName(c.BlockAt(x, y, z))
						props := registry.ParseStateProps(raw)
						for prop, d := range map[string][3]int32{"up": {0, 1, 0}, "down": {0, -1, 0}} {
							n := registryStateName(c.BlockAt(x+d[0], y+d[1], z+d[2]))
							joined := n == "minecraft:chorus_plant" || n == "minecraft:chorus_flower" ||
								prop == "down" && n == "minecraft:end_stone"
							if (props[prop] == "true") != joined {
								t.Fatalf("chunk (%d, %d) (%d,%d,%d): %s = %s next to %s", cx, cz, x, y, z, prop, props[prop], n)
							}
						}
					}
				}
			}
		}
	}
	if plants == 0 || flowers == 0 {
		t.Fatalf("plants = %d, flowers = %d, want both found", plants, flowers)
	}
}

// endWindow generates the chunks from (x0, z0) to (x1, z1) and reads blocks
// across them by world position.
func endWindow(g *EndGenerator, x0, z0, x1, z1 int32) func(x, y, z int32) uint16 {
	chunks := map[ChunkPos]*Chunk{}
	for cz := z0; cz <= z1; cz++ {
		for cx := x0; cx <= x1; cx++ {
			c := newChunk(cx, cz)
			g.Generate(c)
			chunks[ChunkPos{X: cx, Z: cz}] = c
		}
	}
	return func(x, y, z int32) uint16 {
		c, ok := chunks[ChunkPos{X: x >> 4, Z: z >> 4}]
		if !ok {
			return airState
		}
		return c.BlockAt(x&15, y, z&15)
	}
}

// testEndFeatures pins what the features add to two stretches of
// outer islands against the vanilla 26.3 server's own generation of them,
// which tools/worldgen-parity found identical block for block.
func testEndFeatures(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		seed           int64
		x0, z0, x1, z1 int32
		want           map[string]int
		// digest is the FNV-1a of every feature block's position and
		// state, in scan order.
		digest uint64
	}{
		{0, 60, 60, 67, 67, map[string]int{"minecraft:end_stone": 1040, "minecraft:chorus_plant": 509, "minecraft:chorus_flower": 63}, 0xd529be64194146ec},
		{12345, 100, -20, 115, -5, map[string]int{"minecraft:end_stone": 4146, "minecraft:chorus_plant": 584, "minecraft:chorus_flower": 73}, 0x23cca26f1cdc8dcf},
	} {
		g := NewEndGenerator(tc.seed)
		got := map[string]int{}
		h := fnv.New64a()
		for cz := tc.z0; cz <= tc.z1; cz++ {
			for cx := tc.x0; cx <= tc.x1; cx++ {
				c := newChunk(cx, cz)
				g.Generate(c)
				terrain := g.terrainOf(cx, cz)
				for x := range int32(sectionSize) {
					for z := range int32(sectionSize) {
						for y := range int32(endMinY + endHeight) {
							state := c.BlockAt(x, y, z)
							if state == g.endStoneState && terrain.solid(x, y-endMinY, z) || state == airState {
								continue
							}
							got[registryStateName(state)]++
							fmt.Fprint(h, cx*16+x, y, cz*16+z, state)
						}
					}
				}
			}
		}
		if !maps.Equal(got, tc.want) {
			t.Errorf("seed %d: feature blocks %v, want vanilla's %v", tc.seed, got, tc.want)
		}
		if h.Sum64() != tc.digest {
			t.Errorf("seed %d: feature block digest %#x, want %#x", tc.seed, h.Sum64(), tc.digest)
		}
	}
}
