package world

import (
	"bytes"
	"github.com/golem-mc/golem/internal/server/world/terrain"
	"sync"
	"testing"

	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// The overworld generator is a Generator, which is what server.go hands
// NewWorld.
var _ Generator = (*OverworldGenerator)(nil)

// TestOverworldIsDeterministic checks the contract the world's whole
// generate-and-throw-away scheme rests on: the same seed and coordinates
// produce the same chunk, from a fresh generator as much as from one that
// has generated others before, and at negative coordinates as much as at
// positive ones.
func TestOverworldIsDeterministic(t *testing.T) {
	t.Parallel()
	g1 := NewOverworldGenerator(42)
	g2 := NewOverworldGenerator(42)

	for _, pos := range []ChunkPos{{3, -2}, {-1, -1}, {0, 0}, {17, 40}} {
		c1, c2 := newChunk(pos.X, pos.Z), newChunk(pos.X, pos.Z)
		g1.Generate(c1)
		g2.Generate(c2)

		c1.encoded = encodeWhole(c1)
		c2.encoded = encodeWhole(c2)
		if !bytes.Equal(c1.encoded, c2.encoded) {
			t.Fatalf("chunk (%d, %d) differs between two generators of seed 42", pos.X, pos.Z)
		}

		// The light sections make sure the biomes travelled too; the
		// block check walks the whole column of one spot per section.
		for y := int32(worldMinY); y < worldMinY+worldHeight; y += 7 {
			if c1.BlockAt(5, y, 9) != c2.BlockAt(5, y, 9) {
				t.Fatalf("chunk (%d, %d) block at y=%d differs", pos.X, pos.Z, y)
			}
		}
	}
}

// solidFooting reports a block a sea plant can root in: anything but air,
// water or a sea plant, as the surface rules leave bare stone on the floor of
// some oceans.
func solidFooting(g *OverworldGenerator, s uint16) bool {
	switch s {
	case airState, g.waterState, seaStates.seagrass, seaStates.tall[0], seaStates.tall[1],
		seaStates.kelpPlant, seaStates.kelpHead[0], seaStates.kelpHead[1], seaStates.kelpHead[2], seaStates.kelpHead[3]:
		return false
	}
	return true
}

// TestOverworldSeaPlantsAreRooted walks every seagrass, tall seagrass and
// kelp block of a few seeds' chunks around the origin and checks each
// plant's footing: seagrass and the tall pair stand on the sea floor, the tall
// pair's halves on each other, the
// kelp on the floor or the plant below it with the head alone on top, and
// nothing above the water line. The kelp has to reach toward the surface
// somewhere in the sweep, which is what the drawn column heights are for.
func TestOverworldSeaPlantsAreRooted(t *testing.T) {
	t.Parallel()
	var grass, tall, kelp, kelpHigh int
	const seed = 6
	fixture := seaPlantTerrain()
	g := fixture.gen
	for _, c := range fixture.chunks {
		for x := range int32(sectionSize) {
			for z := range int32(sectionSize) {
				for y := int32(waterTop + 2); y >= waterTop-40; y-- {
					at := func(dy int32) uint16 { return c.BlockAt(x, y+dy, z) }
					floor := solidFooting(g, at(-1))
					switch at(0) {
					case seaStates.seagrass:
						grass++
						if !floor {
							t.Errorf("seed %d: seagrass at (%d, %d) y=%d stands on %d, want the sea floor",
								seed, x, z, y, at(-1))
						}
					case seaStates.tall[0]:
						tall++
						if at(1) != seaStates.tall[1] || !floor {
							t.Errorf("seed %d: tall seagrass lower at (%d, %d) y=%d lacks its half or the floor",
								seed, x, z, y)
						}
					case seaStates.tall[1]:
						if at(-1) != seaStates.tall[0] {
							t.Errorf("seed %d: tall seagrass upper at (%d, %d) y=%d stands on %d, want its lower half",
								seed, x, z, y, at(-1))
						}
					case seaStates.kelpPlant, seaStates.kelpHead[0], seaStates.kelpHead[1],
						seaStates.kelpHead[2], seaStates.kelpHead[3]:
						kelp++
						if !floor && at(-1) != seaStates.kelpPlant {
							t.Errorf("seed %d: kelp at (%d, %d) y=%d stands on %d, want the floor or a plant",
								seed, x, z, y, at(-1))
						}
						if at(0) != seaStates.kelpPlant && (at(1) == seaStates.kelpPlant || headOf(at(1))) {
							t.Errorf("seed %d: kelp head at (%d, %d) y=%d is buried under another kelp",
								seed, x, z, y)
						}
						if y >= waterTop-4 {
							kelpHigh++
						}
					}
				}
			}
		}
	}
	if grass == 0 || tall == 0 || kelp == 0 {
		t.Errorf("the sweep found seagrass=%d tall=%d kelp=%d, want all three", grass, tall, kelp)
	}
	if kelpHigh == 0 {
		t.Error("no kelp of the sweep reaches the top five water blocks, want columns reaching toward the surface")
	}
}

// seaStates are the sea plants' block states, as the vegetation features write
// them.
var seaStates = struct {
	seagrass, kelpPlant uint16
	tall                [2]uint16
	kelpHead            [4]uint16
}{
	seagrass:  registry.BlockSeagrass.DefaultState(),
	kelpPlant: registry.BlockKelpPlant.DefaultState(),
	tall:      [2]uint16{registry.BlockTallSeagrass.DefaultState(), mustWith(registry.BlockTallSeagrass.DefaultState(), registry.PropHalf, "upper")},
	kelpHead:  [4]uint16{kelpAged(20), kelpAged(21), kelpAged(22), kelpAged(23)},
}

func mustWith(s uint16, p registry.Prop, v string) uint16 {
	r, _ := registry.WithProp(s, p, v)
	return r
}

func kelpAged(age int) uint16 {
	r, _ := registry.WithInt(registry.BlockKelp.DefaultState(), registry.PropAge, age)
	return r
}

// headOf reports whether the state is one of the kelp heads generation
// writes, ages 20 to 23.
func headOf(state uint16) bool {
	return state == seaStates.kelpHead[0] || state == seaStates.kelpHead[1] ||
		state == seaStates.kelpHead[2] || state == seaStates.kelpHead[3]
}

// TestOverworldKelpColumns measures every kelp column from its top head
// down its stems: it never passes the 1 + below(10) cells the feature
// draws, eleven at most, and a one-block-deep pocket grows the lone head
// straight on the floor, the shallow case where the surface stops the walk
// before a single stem.
func TestOverworldKelpColumns(t *testing.T) {
	t.Parallel()
	const kelpMax = 11
	var columns, tallest, loneHeads int
	const seed = 6
	fixture := seaPlantTerrain()
	g := fixture.gen
	onFloor := func(s uint16) bool {
		return s == g.gravelState || s == g.sandState || s == g.dirtState || s == g.clayState
	}
	for _, c := range fixture.chunks {
		for x := range int32(sectionSize) {
			for z := range int32(sectionSize) {
				for y := int32(waterTop); y >= waterTop-40; y-- {
					if !headOf(c.BlockAt(x, y, z)) {
						continue
					}
					if above := c.BlockAt(x, y+1, z); above == seaStates.kelpPlant || headOf(above) {
						continue // a lower head of the column, not its top
					}
					columns++
					n := int32(1)
					for c.BlockAt(x, y-n, z) == seaStates.kelpPlant {
						n++
					}
					if n > kelpMax {
						t.Errorf("seed %d: a kelp column of %d cells at (%d, %d), want at most %d",
							seed, n, x, z, kelpMax)
					}
					if n > int32(tallest) {
						tallest = int(n)
					}
					if onFloor(c.BlockAt(x, y-1, z)) {
						loneHeads++
					}
				}
			}
		}
	}
	if columns == 0 {
		t.Error("the sweep found no kelp column")
	}
	if loneHeads == 0 {
		t.Error("no top head stands on the floor, want the lone head of a one-block pocket")
	}
	t.Logf("%d columns, tallest %d, %d lone heads in shallows", columns, tallest, loneHeads)
}

// TestOverworldSpawnOnDryLand checks the spawn the world settles on: standing
// on solid ground that is not under water, with room for the player's body.
func TestOverworldSpawnOnDryLand(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	w, _ := NewWorld(dir, NewGenerator(dir, "minecraft:normal", "5", "{}"))
	defer w.Close()

	spawn := w.Spawn()
	if spawn.Y <= worldMinY || spawn.Y >= worldMinY+worldHeight {
		t.Fatalf("spawn Y %d is outside the world", spawn.Y)
	}

	// The chunk the spawn is in, loaded through the world's own plumbing.
	pos := ChunkPos{X: ChunkOf(float64(spawn.X)), Z: ChunkOf(float64(spawn.Z))}
	chunk, _ := w.Acquire(pos)
	defer w.Release(pos)

	feet, head := spawn.Y, spawn.Y+1
	if got := w.BlockAt(spawn.X, feet, spawn.Z); got != airState {
		t.Errorf("block at the spawn's feet = %d, want air", got)
	}
	if got := w.BlockAt(spawn.X, head, spawn.Z); got != airState {
		t.Errorf("block at the spawn's head = %d, want air", got)
	}
	ground := w.BlockAt(spawn.X, spawn.Y-1, spawn.Z)
	water, ok := registry.FindState("minecraft:water", nil)
	if !ok {
		t.Fatal("the block registry has no water")
	}
	if ground == airState || ground == water {
		t.Errorf("spawn ground = %d, want solid dry land (not air, not water)", ground)
	}
	if chunk.BlockAt(spawn.X&15, spawn.Y-1, spawn.Z&15) != ground {
		t.Error("the loaded spawn chunk disagrees with the world's blocks")
	}
}

// TestOverworldSeedSurvivesRestart checks the seed bookkeeping end to end: a
// world created with a level-seed writes that seed into level.dat under
// WorldGenSettings.seed, and a later server starting on the same directory
// uses the level.dat seed over whatever its configuration says, so the same
// world regenerates the same chunks — the whole reason the seed is persisted.
func TestOverworldSeedSurvivesRestart(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	w, _ := NewWorld(dir, NewGenerator(dir, "minecraft:normal", "5", "{}"))
	pos := ChunkPos{X: 1, Z: -2}
	chunk, _ := w.Acquire(pos)
	first := append([]byte(nil), chunk.encoded...)
	firstSpawn := w.Spawn()
	w.Release(pos)
	w.Save()
	w.Close()

	// The seed reached level.dat where the vanilla game keeps it.
	level, err := LoadLevel(dir)
	if err != nil {
		t.Fatalf("LoadLevel: %v", err)
	}
	if !level.HasSeed || level.Seed != 5 {
		t.Fatalf("seed after save = (%d, %v), want the level-seed 5", level.Seed, level.HasSeed)
	}
	if got, _ := nbt.Get[nbt.Long](level.raw, "RandomSeed"); got != 5 {
		t.Errorf("RandomSeed = %d, want 5 written for the older format to read", got)
	}
	settings, ok := nbt.Get[nbt.Compound](level.raw, "WorldGenSettings")
	if !ok {
		t.Fatal("level.dat has no WorldGenSettings compound")
	}
	if got, _ := nbt.Get[nbt.Long](settings, "seed"); got != 5 {
		t.Errorf("WorldGenSettings.seed = %d, want 5", got)
	}
	if got, _ := nbt.Get[nbt.Int](level.raw, "Generator"); got != generatorDefault {
		t.Errorf("Generator = %d, want %d (the overworld, not the flat default)", got, generatorDefault)
	}

	// A restart with a different configured seed keeps the world's own:
	// level.dat is the record, level-seed only shapes a world being made.
	again, _ := NewWorld(dir, NewGenerator(dir, "minecraft:normal", "999999", "{}"))
	defer again.Close()
	if got := again.level.Seed; got != 5 {
		t.Errorf("seed after restart = %d, want the level.dat seed 5", got)
	}
	chunk2, _ := again.Acquire(pos)
	defer again.Release(pos)
	if !bytes.Equal(first, chunk2.encoded) {
		t.Error("the same chunk regenerated differently after the restart")
	}
	if got := again.Spawn(); got != firstSpawn {
		t.Errorf("spawn after restart = %+v, want the settled spawn %+v", got, firstSpawn)
	}
}

// TestParseSeed checks the reading of a level-seed value: numbers are seeds
// and anything else is hashed the way Java hashes.
func TestParseSeed(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		spec string
		want int64
	}{
		{"5", 5},
		{"-42", -42},
		{" 123 ", 123}, // a stray space is a typo, not a different seed
		{"hello", 99162322},
		// Too big for a 64-bit long: the game hashes it instead of
		// refusing it, and so do we.
		{"9223372036854775808", -1773151197},
		{"Glacier", 1772835215},
	} {
		if got := ParseSeed(tc.spec); got != tc.want {
			t.Errorf("ParseSeed(%q) = %d, want %d", tc.spec, got, tc.want)
		}
	}
}

// TestNewGeneratorChoosesByLevelType checks the factory the server's one
// configuration line goes through: the kind of world picks the generator,
// and a seed is settled early or deferred to the world. The directory is an
// empty one, standing for a world that records no generator of its own yet.
func TestNewGeneratorChoosesByLevelType(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	if !isFlatGenerator(NewGenerator(dir, "minecraft:flat", "", "{}")) {
		t.Error("level-type minecraft:flat did not give the flat generator")
	}
	if !isFlatGenerator(NewGenerator(dir, "flat", "", "{}")) {
		t.Error("level-type flat did not give the flat generator")
	}

	g, ok := NewGenerator(dir, "minecraft:normal", "42", "{}").(*OverworldGenerator)
	if !ok {
		t.Fatal("level-type minecraft:normal did not give the overworld generator")
	}
	if g.seed != 42 || g.deferred {
		t.Errorf("level-seed 42 gave seed %d (deferred=%v), want 42 settled", g.seed, g.deferred)
	}
	// A cave entrance can open the surface well below sea level (seed 42
	// has one at the origin), so only the build range bounds it.
	if got := g.SurfaceHeight(0, 0); got < worldMinY || got >= worldMinY+worldHeight {
		t.Errorf("SurfaceHeight(0, 0) = %d, outside the world's range", got)
	}

	deferred, ok := NewGenerator(dir, "minecraft:normal", "", "{}").(*OverworldGenerator)
	if !ok || !deferred.deferred {
		t.Error("an empty level-seed did not defer the overworld's seed to the world")
	}

	// A text seed is hashed, not parsed: it shapes a world being created
	// exactly like the number another operator picks.
	if g, ok := NewGenerator(dir, "normal", "hello", "{}").(*OverworldGenerator); !ok || g.seed != 99162322 {
		t.Errorf("level-seed %q gave seed %d, want Java's hash 99162322", "hello", g.seed)
	}
	// The preset is lowercased and namespaced, and a deferred seed keeps it.
	for levelType, want := range map[string]overworldPreset{
		"minecraft:amplified": presetAmplified, "Large_Biomes": presetLargeBiomes, "single_biome_surface": presetSingleBiome,
		"largebiomes": presetNormal, "default": presetNormal,
	} {
		for _, seed := range []string{"7", ""} {
			g, ok := NewGenerator(dir, levelType, seed, "{}").(*OverworldGenerator)
			if !ok {
				t.Errorf("level-type %q did not give the overworld generator", levelType)
			} else if g.preset != want {
				t.Errorf("level-type %q (level-seed %q) gave preset %d, want %d", levelType, seed, g.preset, want)
			}
		}
	}
}

// BenchmarkOverworldGenerate measures one column of the world being made:
// heightmap, terrain, caves, ores, trees and biomes.
func BenchmarkOverworldGenerate(b *testing.B) {
	g := NewOverworldGenerator(5)
	g.Generate(newChunk(1000, 1000)) // one-time setup (stronghold ring, jigsaw data) stays out of the timing
	b.ReportAllocs()
	b.ResetTimer()
	i := int32(0)
	for b.Loop() {
		c := newChunk(i%64, i/64%64)
		g.Generate(c)
		i++
	}
}

// joinViewOrder is the chunks of a (2r+1)^2 view, closest first.
func joinViewOrder(cx, cz, r int32) []ChunkPos {
	var out []ChunkPos
	for ring := int32(0); ring <= r; ring++ {
		for x := -ring; x <= ring; x++ {
			for z := -ring; z <= ring; z++ {
				if max(absInt32(x), absInt32(z)) == ring {
					out = append(out, ChunkPos{X: cx + x, Z: cz + z})
				}
			}
		}
	}
	return out
}

// BenchmarkOverworldJoinGenerate generates one player's 17x17 view closest first on
// 8 workers, the pool's bound, from a cold generator.
func BenchmarkOverworldJoinGenerate(b *testing.B) {
	NewOverworldGenerator(5).Generate(newChunk(1000, 1000)) // process-wide structure data
	order := joinViewOrder(0, 0, 8)
	var fills int64
	b.ReportAllocs()
	for b.Loop() {
		b.StopTimer()
		g := NewOverworldGenerator(5)
		g.Generate(newChunk(1000, 1000))
		f0 := g.biomeFills.Load()
		b.StartTimer()
		jobs := make(chan ChunkPos, len(order))
		for _, p := range order {
			jobs <- p
		}
		close(jobs)
		var wg sync.WaitGroup
		for range 8 {
			wg.Go(func() {
				for p := range jobs {
					g.Generate(newChunk(p.X, p.Z))
				}
			})
		}
		wg.Wait()
		fills += g.biomeFills.Load() - f0
	}
	b.ReportMetric(float64(fills)/float64(b.N*len(order)), "biomefills/chunk")
}

// BenchmarkOverworldGenerateAroundStart walks the 9x9 chunks around the seed
// 12345 ancient city at (-18, -18), where every chunk's terrain meets the
// city's beardifier and the city's chunks grow sculk patches.
func BenchmarkOverworldGenerateAroundStart(b *testing.B) {
	g := NewOverworldGenerator(12345)
	g.Generate(newChunk(1000, 1000)) // one-time setup (stronghold ring, jigsaw data) stays out of the timing
	b.ReportAllocs()
	b.ResetTimer()
	i := int32(0)
	for b.Loop() {
		c := newChunk(-22+i%9, -22+i/9%9)
		g.Generate(c)
		i++
	}
}

// BenchmarkOverworldGenerateDripstoneCaves walks the 9x9 chunks around seed 5's
// dripstone caves at (32, -104), exercising large dripstone placement.
func BenchmarkOverworldGenerateDripstoneCaves(b *testing.B) {
	g := NewOverworldGenerator(5)
	g.Generate(newChunk(1000, 1000)) // one-time setup (stronghold ring, jigsaw data) stays out of the timing
	b.ReportAllocs()
	b.ResetTimer()
	i := int32(0)
	for b.Loop() {
		c := newChunk(28+i%9, -108+i/9%9)
		g.Generate(c)
		i++
	}
}

// BenchmarkOverworldGenerateSulfurCaves walks the 9x9 chunks around seed 5's
// sulfur caves at (-16, -21), exercising sulfur springs and pools.
func BenchmarkOverworldGenerateSulfurCaves(b *testing.B) {
	g := NewOverworldGenerator(5)
	g.Generate(newChunk(1000, 1000)) // one-time setup (stronghold ring, jigsaw data) stays out of the timing
	b.ReportAllocs()
	b.ResetTimer()
	i := int32(0)
	for b.Loop() {
		c := newChunk(-20+i%9, -25+i/9%9)
		g.Generate(c)
		i++
	}
}

// BenchmarkOverworldGenerateAndLight measures the pair of them together —
// generating a chunk and computing its light, through the world's own acquire
// path — which is what a player's chunk stream actually pays.
func BenchmarkOverworldGenerateAndLight(b *testing.B) {
	dir := b.TempDir()
	w, _ := NewWorld(dir, NewGenerator(dir, "minecraft:normal", "5", "{}"))
	defer w.Close()
	b.ReportAllocs()
	b.ResetTimer()
	i := int32(0)
	for b.Loop() {
		pos := ChunkPos{X: i % 32, Z: i / 32 % 32}
		w.Acquire(pos)
		w.Release(pos)
		i++
	}
}

// TestOverworldOreVeinsCrossChunkBorders checks that veins are blobs that run
// over chunk borders: the columns at a chunk's edge hold no more ore than its
// inner columns, and the same vein shows on both sides of a border.
func TestOverworldOreVeinsCrossChunkBorders(t *testing.T) {
	t.Parallel()
	g := NewOverworldGenerator(5)
	// Border counts depend on decoration order, so other readers cannot warm this cache.
	terrain := newTestTerrain(5, false)
	// Nine adjacent pairs cover both boundary columns and thousands of inner cells.

	var edge, inner, paired int
	for cx := int32(-1); cx <= 1; cx++ {
		for cz := int32(-1); cz <= 1; cz++ {
			a, b := newChunk(cx, cz), newChunk(cx+1, cz)
			terrain.Generate(a)
			terrain.Generate(b)
			for y := int32(worldMinY + 10); y < 100; y++ {
				for z := range int32(16) {
					for x := range int32(16) {
						s := a.BlockAt(x, y, z)
						if s != g.coalOreState && s != g.deepslateCoalOreState {
							continue
						}
						if x == 0 || x == 15 {
							edge++
						} else {
							inner++
						}
					}
					if (a.BlockAt(15, y, z) == g.coalOreState) && (b.BlockAt(0, y, z) == g.coalOreState) {
						paired++
					}
				}
			}
		}
	}
	if perEdge, perInner := float64(edge)/2, float64(inner)/14; perEdge > 1.5*perInner {
		t.Errorf("edge columns hold %.1f coal per column against %.1f in the inner ones, want no pile-up at borders", perEdge, perInner)
	}
	if paired < 10 {
		t.Errorf("%d coal blocks continue over a chunk border, want veins that cross it", paired)
	}
}

// TestOverworldChunkBiomesAreTheBiomeSource checks that a generated chunk
// carries the multi-noise biome source's biome, by registry name, in every
// 4x4x4 cell, and that the cells vary across the chunks and up their columns
// (the cave biomes). The source itself is pinned to vanilla by levelgen's
// golden test.
func TestOverworldChunkBiomesAreTheBiomeSource(t *testing.T) {
	t.Parallel()
	for _, seed := range []int64{0, 12345} {
		g := NewOverworldGenerator(seed)
		g.DisableStructures()
		names := map[string]bool{}
		columns := 0
		// Mixed-sign and distant coordinates exercise full vertical quart grids
		// through the surface and cave biomes required by the assertions.
		for _, pos := range []ChunkPos{{0, 0}, {-3, 7}, {40, -25}, {-90, -61}, {12, 12}, {300, 41}} {
			c := newChunk(pos.X, pos.Z)
			g.Generate(c)
			g.biomeSource.FillChunk(pos.X, pos.Z, func(x, y, z int32, biome terrain.OverworldBiome) {
				want := "minecraft:" + biome.Name()
				got := registry.Biome(c.BiomeAt(x*4, worldMinY+y*4, z*4)).Name()
				if got != want {
					t.Fatalf("seed %d chunk %v cell (%d, %d, %d): biome %s, want %s", seed, pos, x, y, z, got, want)
				}
				names[got] = true
				if y > 0 && got != registry.Biome(c.BiomeAt(x*4, worldMinY+(y-1)*4, z*4)).Name() {
					columns++
				}
			})
		}
		if len(names) < 3 || columns == 0 {
			t.Errorf("seed %d: %d distinct biomes and %d changes up a column, want surface and cave biomes", seed, len(names), columns)
		}
	}
}

// Seed 6's 3x3 ocean window at (0..2, -4..-2) contains all sea-plant kinds,
// kelp reaching the surface and lone heads in shallows. Both tests only read it.
type seaPlantFixture struct {
	gen    *OverworldGenerator
	chunks []*Chunk
}

var seaPlantTerrain = sync.OnceValue(func() seaPlantFixture {
	fixture := seaPlantFixture{gen: NewOverworldGenerator(6)}
	fixture.gen.DisableStructures()
	for cx := int32(0); cx <= 2; cx++ {
		for cz := int32(-4); cz <= -2; cz++ {
			c := newChunk(cx, cz)
			fixture.gen.Generate(c)
			fixture.chunks = append(fixture.chunks, c)
		}
	}
	return fixture
})
