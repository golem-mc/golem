package world

import (
	"fmt"
	"slices"
	"testing"

	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// TestTheFlatPresetWorldIsVanillasDefault opens a fresh world on the flat
// level-type and reads back what its creation wrote: a spawn at the first
// air block above the preset's ground, and a world_gen_settings.dat whose
// layers name the vanilla default flat preset's blocks in the heights the
// generator fills them, so vanilla reading the world generates the ground
// this server does.
func TestTheFlatPresetWorldIsVanillasDefault(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	// The generator is built before the world opens, so the layer check
	// below reads the world's settings rather than echoing the generator
	// the world was opened with.
	og := NewGenerator(dir, "minecraft:flat", "", "{}").(*OverworldGenerator)
	w, _ := NewWorld(dir, og)
	gen := og.Flat()
	defer w.Close()

	if got := w.Spawn(); got != (SpawnPos{X: 0, Y: -60, Z: 0}) {
		t.Errorf("spawn = %+v, want (0, -60, 0)", got)
	}

	stored, err := readSavedData(dir, worldGenFile)
	if err != nil {
		t.Fatal(err)
	}
	generator, ok := overworldGenerator(stored)
	if !ok {
		t.Fatal("world_gen_settings.dat has no overworld generator")
	}
	if kind, _ := nbt.Get[nbt.String](generator, "type"); string(kind) != "minecraft:flat" {
		t.Fatalf("overworld generator = %q, want minecraft:flat", kind)
	}
	settings, _ := nbt.Get[nbt.Compound](generator, "settings")
	if biome, _ := nbt.Get[nbt.String](settings, "biome"); string(biome) != "minecraft:plains" {
		t.Errorf("flat biome = %q, want minecraft:plains", biome)
	}
	layers, _ := nbt.Get[nbt.List](settings, "layers")

	// The layers are the ones the generator fills, walked the same way the
	// generator walks them: block for block from the bottom of the world up.
	c := newChunk(0, 0)
	gen.Generate(c)
	preset := []struct {
		block  string
		height nbt.Int
	}{
		{"minecraft:bedrock", 1},
		{"minecraft:dirt", 2},
		{"minecraft:grass_block", 1},
	}
	y := int32(worldMinY)
	for i, item := range layers.Items {
		layer := item.(nbt.Compound)
		block, _ := nbt.Get[nbt.String](layer, "block")
		height, _ := nbt.Get[nbt.Int](layer, "height")
		if i >= len(preset) || string(block) != preset[i].block || height != preset[i].height {
			t.Errorf("layer %d = %s×%d, want the vanilla default preset's", i, block, height)
			continue
		}
		for range height {
			if got := c.BlockAt(0, y, 0); got != registry.BlockStates[string(block)] {
				t.Fatalf("block at y=%d is %d, the settings say %s", y, got, block)
			}
			y++
		}
	}
	if len(layers.Items) != len(preset) || y != -60 || c.BlockAt(0, y, 0) != airState {
		t.Errorf("the layers end at y=%d across %d entries, want %d ending in air at -60", y, len(layers.Items), len(preset))
	}
}

// TestTheFlatPresetWorldKeepsItsStoredGround opens a world whose
// world_gen_settings.dat records a flat preset of its own, and reads the
// generator back out of it: the layers and biome the file records, not the
// vanilla default preset the level-type alone would make.
func TestTheFlatPresetWorldKeepsItsStoredGround(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := writeSavedData(dir, worldGenFile, flatWorldGenSettings(0, newFlatGenerator("minecraft:desert", []flatLayer{
		{state: registry.MustState("minecraft:stone", nil), height: 128},
	}), false)); err != nil {
		t.Fatal(err)
	}

	gen, ok := NewGenerator(dir, "minecraft:flat", "", "{}").(*FlatGenerator)
	if !ok {
		t.Fatal("NewGenerator did not keep the stored flat generator")
	}
	if gen.biomeName != "minecraft:desert" || len(gen.layers) != 1 || gen.layers[0].height != 128 {
		t.Errorf("generator = %q %+v, want minecraft:desert with one layer 128 high", gen.biomeName, gen.layers)
	}
	// The stored type wins over the asked-for one too, so a world made flat
	// keeps being flat whatever server.properties says now.
	if g, ok := NewGenerator(dir, "minecraft:normal", "", "{}").(*FlatGenerator); !ok || g.biomeName != "minecraft:desert" {
		t.Error("NewGenerator lost the stored flat generator to the asked-for level type")
	}
}

// TestAStoredPresetWithAnUnknownBiomeKeepsToThePlains opens a world whose
// stored flat preset names a biome the registries lack, and reads the
// generator back: the layers kept, and the biome fallen back to the plains
// vanilla's codec keeps an unknown name to.
// vanilla: FlatLevelGeneratorSettings
func TestAStoredPresetWithAnUnknownBiomeKeepsToThePlains(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := writeSavedData(dir, worldGenFile, nbt.Compound{
		"seed": nbt.Long(0),
		"dimensions": nbt.Compound{
			DimensionName: nbt.Compound{
				"type": nbt.String(DimensionType),
				"generator": nbt.Compound{
					"type": nbt.String("minecraft:flat"),
					"settings": nbt.Compound{
						"biome": nbt.String("minecraft:nowhere"),
						"layers": nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{
							nbt.Compound{"block": nbt.String("minecraft:stone"), "height": nbt.Int(8)},
						}},
					},
				},
			},
		},
	}); err != nil {
		t.Fatal(err)
	}

	gen := flatOf(NewGenerator(dir, "minecraft:flat", "", "{}"))
	if gen == nil {
		t.Fatal("NewGenerator did not keep the stored flat generator")
	}
	if want := registry.MustID("minecraft:worldgen/biome", "minecraft:plains"); gen.biomeName != "minecraft:plains" || gen.biome != want {
		t.Errorf("generator biome = %q (%d), want the plains fallback (%d)", gen.biomeName, gen.biome, want)
	}
	if len(gen.layers) != 1 || gen.layers[0].height != 8 {
		t.Errorf("generator layers = %+v, want the stored one, 8 high", gen.layers)
	}
}

// TestAStoredPresetWithAnUnknownBlockFallsBackToTheDefault opens a world
// whose stored flat preset names a block the registries lack, and reads the
// generator back: the vanilla default preset, which NewGenerator answers a
// rejected preset with, as vanilla's codec rejects the file.
// vanilla: FlatLevelGeneratorSettings
func TestAStoredPresetWithAnUnknownBlockFallsBackToTheDefault(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := writeSavedData(dir, worldGenFile, nbt.Compound{
		"seed": nbt.Long(0),
		"dimensions": nbt.Compound{
			DimensionName: nbt.Compound{
				"type": nbt.String(DimensionType),
				"generator": nbt.Compound{
					"type": nbt.String("minecraft:flat"),
					"settings": nbt.Compound{
						"biome": nbt.String("minecraft:plains"),
						"layers": nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{
							nbt.Compound{"block": nbt.String("minecraft:nowhere"), "height": nbt.Int(8)},
						}},
					},
				},
			},
		},
	}); err != nil {
		t.Fatal(err)
	}

	gen := flatOf(NewGenerator(dir, "minecraft:flat", "", "{}"))
	if gen == nil {
		t.Fatal("NewGenerator did not keep the flat level-type")
	}
	want := NewVanillaFlatGenerator()
	if gen.biomeName != want.biomeName || len(gen.layers) != len(want.layers) {
		t.Fatalf("generator = %q %+v, want the vanilla default preset %q %+v", gen.biomeName, gen.layers, want.biomeName, want.layers)
	}
	for i, layer := range gen.layers {
		if layer != want.layers[i] {
			t.Errorf("layer %d = %+v, want %+v", i, layer, want.layers[i])
		}
	}
}

// TestAStoredPresetWithAZeroHeightLayerAndNoBiomeIsAccepted reads a stored
// preset the codec allows: a height-0 layer adds nothing and a missing biome
// is plains.
// vanilla: FlatLayerInfo.CODEC, FlatLevelGeneratorSettings.getBiome
func TestAStoredPresetWithAZeroHeightLayerAndNoBiomeIsAccepted(t *testing.T) {
	t.Parallel()
	settings := nbt.Compound{
		"dimensions": nbt.Compound{
			DimensionName: nbt.Compound{
				"generator": nbt.Compound{
					"type": nbt.String("minecraft:flat"),
					"settings": nbt.Compound{
						"layers": nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{
							nbt.Compound{"block": nbt.String("minecraft:sand"), "height": nbt.Int(0)},
							nbt.Compound{"block": nbt.String("minecraft:stone"), "height": nbt.Int(3)},
						}},
					},
				},
			},
		},
	}
	gen, ok := flatPresetOf(settings)
	if !ok {
		t.Fatal("flatPresetOf rejected the preset")
	}
	if gen.biomeName != "minecraft:plains" || len(gen.layers) != 1 || gen.layers[0].height != 3 {
		t.Errorf("generator = %q %+v, want plains with one stone layer 3 high", gen.biomeName, gen.layers)
	}
}

// TestATallFlatPresetSpawnsAtTheTopOfTheWorld checks that layers taller than
// the world put the spawn at its top.
// vanilla: FlatLevelSource.getSpawnHeight
func TestATallFlatPresetSpawnsAtTheTopOfTheWorld(t *testing.T) {
	t.Parallel()
	g := flatGeneratorFromSettings(`{"layers":[{"block":"stone","height":400}]}`)
	if got := g.spawnY(); got != worldMinY+worldHeight {
		t.Errorf("spawnY = %d, want %d", got, worldMinY+worldHeight)
	}
}

// TestAnEmptyStoredPresetIsAVoidWorld checks that a stored preset with no
// layers reads back as a void world rather than being rejected.
// vanilla: FlatLevelGeneratorSettings.CODEC
func TestAnEmptyStoredPresetIsAVoidWorld(t *testing.T) {
	t.Parallel()
	void := flatGeneratorFromSettings(`{"layers":[],"biome":"desert"}`)
	gen, ok := flatPresetOf(worldGenSettingsFor(0, flatGeneratorSettings(void), false))
	if !ok {
		t.Fatal("flatPresetOf rejected the empty preset")
	}
	if len(gen.layers) != 0 || gen.biomeName != "minecraft:desert" {
		t.Errorf("generator = %q %+v, want a desert with no layers", gen.biomeName, gen.layers)
	}
}

// flatOf is the flat preset a flat world's generator makes, nil for another
// generator.
func flatOf(gen Generator) *FlatGenerator {
	switch g := gen.(type) {
	case *FlatGenerator:
		return g
	case *OverworldGenerator:
		return g.Flat()
	}
	return nil
}

// flatVillage is the nearest plains village start of the default flat world
// for seed 0, and the generator that grew it.
func flatVillage(t *testing.T) (*OverworldGenerator, *structureStart) {
	t.Helper()
	g := newFlatOverworldGenerator(0, NewVanillaFlatGenerator())
	bx, bz, id, ok := g.NearestStructure([]string{"minecraft:village_plains"}, 0, 64, 0, 100)
	if !ok || id != "minecraft:village_plains" {
		t.Fatalf("NearestStructure = %d %d %q %v, want a plains village", bx, bz, id, ok)
	}
	for _, s := range g.structureStartsIn(bx>>4, bz>>4) {
		if s.def.Name == "minecraft:village_plains" {
			return g, s
		}
	}
	t.Fatalf("no village start in chunk (%d, %d)", bx>>4, bz>>4)
	return nil, nil
}

// TestADefaultFlatWorldGrowsAVillageOnItsGrass checks that the default flat
// preset places villages: the nearest start's chunk holds village blocks
// standing on the grass at y=-61, the ground being the preset's layers.
// vanilla: FlatLevelGeneratorSettings.getDefault, FlatLevelSource
func TestADefaultFlatWorldGrowsAVillageOnItsGrass(t *testing.T) {
	t.Parallel()
	g, start := flatVillage(t)
	c := newChunk(start.chunkX, start.chunkZ)
	g.Generate(c)
	village := 0
	for x := range int32(16) {
		for z := range int32(16) {
			if c.BlockAt(x, -64, z) != registry.BlockBedrock.DefaultState() {
				t.Fatalf("block at (%d, -64, %d) is not the bedrock layer", x, z)
			}
			// Paths replace the grass; anything above it is the village's.
			if b := registry.BlockOf(c.BlockAt(x, -61, z)); b != registry.BlockGrassBlock && b != registry.BlockDirt {
				village++
			}
			if !overworldAirBlocks[registry.BlockOf(c.BlockAt(x, -60, z))] {
				village++
			}
		}
	}
	if village < 16 {
		t.Errorf("chunk (%d, %d) holds %d village blocks on the grass, want at least 16", start.chunkX, start.chunkZ, village)
	}
	if got := c.BiomeAt(0, -60, 0); got != registry.MustID("minecraft:worldgen/biome", "minecraft:plains") {
		t.Errorf("biome = %d, want plains", got)
	}
}

// TestAFlatWorldWithoutStructuresGrowsNoVillage checks that the same world
// with structures disabled grows no start and lays only its layers.
func TestAFlatWorldWithoutStructuresGrowsNoVillage(t *testing.T) {
	t.Parallel()
	_, start := flatVillage(t)
	g := newFlatOverworldGenerator(0, NewVanillaFlatGenerator())
	g.DisableStructures()
	if starts := g.structureStartsIn(start.chunkX, start.chunkZ); len(starts) != 0 {
		t.Fatalf("structureStartsIn = %d starts, want none", len(starts))
	}
	c := newChunk(start.chunkX, start.chunkZ)
	g.Generate(c)
	for x := range int32(16) {
		for z := range int32(16) {
			if c.BlockAt(x, -61, z) != registry.BlockGrassBlock.DefaultState() || c.BlockAt(x, -60, z) != airState {
				t.Fatalf("column (%d, %d) is not the bare preset", x, z)
			}
		}
	}
}

// TestAFlatWorldLaysItsStrongholdsOutWithSeedZero checks that a flat world's
// stronghold chunks are the rings of seed 0, whatever the level seed, each
// moved by a random offset since its one biome is preferred.
// vanilla: ChunkGeneratorStructureState.createForFlat, FixedBiomeSource.findBiomeHorizontal
func TestAFlatWorldLaysItsStrongholdsOutWithSeedZero(t *testing.T) {
	t.Parallel()
	g := newFlatOverworldGenerator(12345, NewVanillaFlatGenerator())
	// A vanilla 26.3 server, level-type=minecraft:flat and level-seed=12345,
	// answers /locate structure minecraft:stronghold from (0, -1500), (1500, 0)
	// and (0, 1500) with [-304, ~, -1728], [2032, ~, 784] and [-1520, ~, 1168].
	want := [][2]int32{{-19, -108}, {127, 49}, {-95, 73}}
	if got := g.strongholdChunks()[:3]; !slices.Equal(got, want) {
		t.Errorf("first ring's stronghold chunks %v, want %v", got, want)
	}
}

// TestAFlatWorldLocatesItsVillagesOnly checks the locator of a default flat
// world: it finds plains villages and none of the sets the preset leaves out.
func TestAFlatWorldLocatesItsVillagesOnly(t *testing.T) {
	t.Parallel()
	var locator StructureLocator = newFlatOverworldGenerator(0, NewVanillaFlatGenerator())
	if _, _, _, ok := locator.NearestStructure([]string{"minecraft:village_plains"}, 0, 64, 0, 100); !ok {
		t.Error("the default flat world locates no plains village")
	}
	if _, _, _, ok := locator.NearestStructure([]string{"minecraft:pillager_outpost"}, 0, 64, 0, 100); ok {
		t.Error("the default flat world locates a pillager outpost its preset leaves out")
	}
}

// TestFlatStructureOverridesAreReadAndWritten checks generator-settings'
// structure_overrides: a list, a single id, an absent key meaning every set
// and a tag meaning none, and that the saved settings read back the same.
// vanilla: FlatLevelGeneratorSettings.CODEC, FlatLevelSource.createState
func TestFlatStructureOverridesAreReadAndWritten(t *testing.T) {
	t.Parallel()
	const layers = `"layers":[{"block":"stone","height":4}]`
	for _, tc := range []struct {
		settings string
		sets     []string
		all      bool
	}{
		{`{` + layers + `,"structure_overrides":["villages","minecraft:strongholds"]}`, []string{"minecraft:villages", "minecraft:strongholds"}, false},
		{`{` + layers + `,"structure_overrides":"minecraft:villages"}`, []string{"minecraft:villages"}, false},
		{`{` + layers + `}`, nil, true},
		{`{` + layers + `,"structure_overrides":"#minecraft:villages"}`, nil, false},
		{`{` + layers + `,"structure_overrides":[]}`, nil, false},
	} {
		g := flatGeneratorFromSettings(tc.settings)
		if !slices.Equal(g.structureSets, tc.sets) || g.allStructureSets != tc.all {
			t.Errorf("%s: sets %v all %v, want %v %v", tc.settings, g.structureSets, g.allStructureSets, tc.sets, tc.all)
		}
		saved := flatGeneratorSettings(g)
		_, hasKey := nbt.Get[nbt.List](saved["settings"].(nbt.Compound), "structure_overrides")
		if hasKey == tc.all {
			t.Errorf("%s: saved structure_overrides present %v, want %v", tc.settings, hasKey, !tc.all)
		}
		back, ok := flatPresetOf(worldGenSettingsFor(0, saved, true))
		if !ok || !slices.Equal(back.structureSets, tc.sets) || back.allStructureSets != tc.all {
			t.Errorf("%s: read back %v all %v", tc.settings, back.structureSets, back.allStructureSets)
		}
		if _, flatMode := flatGenerator(g, "1").(*OverworldGenerator); flatMode != (tc.all || len(tc.sets) > 0) {
			t.Errorf("%s: flat-mode overworld %v, want it only with sets", tc.settings, flatMode)
		}
	}
}

// TestADefaultFlatWorldRecordsItsStructures opens a default flat world and
// reads back its world_gen_settings.dat: its sets and generate_structures, and
// that the world reports itself flat.
func TestADefaultFlatWorldRecordsItsStructures(t *testing.T) {
	t.Parallel()
	for _, disabled := range []bool{false, true} {
		dir := t.TempDir()
		g := NewGenerator(dir, "minecraft:flat", "0", "{}").(*OverworldGenerator)
		if disabled {
			g.DisableStructures()
		}
		w, _ := NewWorld(dir, g)
		if !w.IsFlat() || w.SeaLevel() != -63 || w.Spawn() != (SpawnPos{X: 0, Y: -60, Z: 0}) {
			t.Errorf("flat %v sea level %d spawn %+v, want a flat world at -63 spawning at (0, -60, 0)", w.IsFlat(), w.SeaLevel(), w.Spawn())
		}
		w.Close()
		stored, err := readSavedData(dir, worldGenFile)
		if err != nil {
			t.Fatal(err)
		}
		if got, _ := nbt.Get[nbt.Byte](stored, "generate_structures"); (got == 1) == disabled {
			t.Errorf("disabled %v: generate_structures = %d", disabled, got)
		}
		flat, ok := flatPresetOf(stored)
		if !ok || !slices.Equal(flat.structureSets, []string{"minecraft:strongholds", "minecraft:villages"}) {
			t.Errorf("disabled %v: stored sets %v", disabled, flat.structureSets)
		}
	}
}

// TestFlatFeaturesAndLakesAreReadAndWritten checks generator-settings'
// features and lakes: read, saved and read back, and either alone makes the
// flat-mode overworld even without structure sets.
// vanilla: FlatLevelGeneratorSettings.CODEC
func TestFlatFeaturesAndLakesAreReadAndWritten(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ features, lakes bool }{{false, false}, {true, false}, {false, true}, {true, true}} {
		settings := fmt.Sprintf(`{"layers":[{"block":"stone","height":4}],"structure_overrides":[],"features":%v,"lakes":%v}`, tc.features, tc.lakes)
		g := flatGeneratorFromSettings(settings)
		if g.features != tc.features || g.lakes != tc.lakes {
			t.Errorf("%s: features %v lakes %v", settings, g.features, g.lakes)
		}
		back, ok := flatPresetOf(worldGenSettingsFor(0, flatGeneratorSettings(g), true))
		if !ok || back.features != tc.features || back.lakes != tc.lakes {
			t.Errorf("%s: read back features %v lakes %v", settings, back.features, back.lakes)
		}
		if _, flatMode := flatGenerator(g, "1").(*OverworldGenerator); flatMode != (tc.features || tc.lakes) {
			t.Errorf("%s: flat-mode overworld %v", settings, flatMode)
		}
	}
}

// TestAForestFlatWorldWithFeaturesGrowsTrees checks that features places the
// biome's own decoration: a forest flat world grows oak or birch logs.
// vanilla: FlatLevelGeneratorSettings.adjustGenerationSettings
func TestAForestFlatWorldWithFeaturesGrowsTrees(t *testing.T) {
	t.Parallel()
	g := NewFlatOverworldGeneratorFor(0, `{"biome":"minecraft:forest","features":true,"structure_overrides":[],"layers":[{"block":"minecraft:bedrock","height":1},{"block":"minecraft:dirt","height":3},{"block":"minecraft:grass_block","height":1}]}`)
	logs := 0
	for cx := range int32(2) {
		for cz := range int32(2) {
			c := newChunk(cx, cz)
			g.Generate(c)
			for i := range c.sections {
				palette, _ := c.sections[i].blockPalette()
				for _, s := range palette {
					if b := registry.BlockOf(s); b == registry.BlockOakLog || b == registry.BlockBirchLog {
						logs++
					}
				}
			}
		}
	}
	if logs == 0 {
		t.Error("a forest flat world with features grows no oak or birch log in 2x2 chunks")
	}
}

// TestAFlatWorldWithLakesHasALavaLake checks that lakes places the lava lakes:
// some chunk of a 16x16 area holds lava.
// vanilla: FlatLevelGeneratorSettings.adjustGenerationSettings (lakes)
func TestAFlatWorldWithLakesHasALavaLake(t *testing.T) {
	t.Parallel()
	g := NewFlatOverworldGeneratorFor(0, `{"lakes":true,"structure_overrides":[],"layers":[{"block":"minecraft:bedrock","height":1},{"block":"minecraft:dirt","height":3},{"block":"minecraft:grass_block","height":1}]}`)
	for cx := range int32(16) {
		for cz := range int32(16) {
			c := newChunk(cx, cz)
			g.Generate(c)
			for i := range c.sections {
				if palette, _ := c.sections[i].blockPalette(); slices.ContainsFunc(palette, func(s uint16) bool { return registry.BlockOf(s) == registry.BlockLava }) {
					return
				}
			}
		}
	}
	t.Error("a flat world with lakes holds no lava in 16x16 chunks")
}

// TestASnowTopLayerIsFilledOverTheVillage checks a layer list ending in one
// snow layer: the snow is not part of the base, so the village stands on the
// grass, and every air cell at the snow's y is filled with snow after it.
// vanilla: FlatLevelGeneratorSettings.adjustGenerationSettings, FillLayerFeature, FlatLevelSource.getBaseHeight
func TestASnowTopLayerIsFilledOverTheVillage(t *testing.T) {
	t.Parallel()
	g := NewFlatOverworldGeneratorFor(0, `{"structure_overrides":["minecraft:villages"],"layers":[{"block":"minecraft:bedrock","height":1},{"block":"minecraft:dirt","height":2},{"block":"minecraft:grass_block","height":1},{"block":"minecraft:snow","height":1}]}`)
	if got := g.height(0, 0); got != -61 {
		t.Fatalf("base height %d, want the grass at -61", got)
	}
	bx, bz, _, ok := g.NearestStructure([]string{"minecraft:village_plains"}, 0, 64, 0, 100)
	if !ok {
		t.Fatal("no plains village")
	}
	c := newChunk(bx>>4, bz>>4)
	g.Generate(c)
	snow, village := 0, 0
	for x := range int32(16) {
		for z := range int32(16) {
			switch s := c.BlockAt(x, -60, z); {
			case s == snowByLayers[1]:
				snow++
			case overworldAirBlocks[registry.BlockOf(s)]:
				t.Fatalf("block at (%d, -60, %d) is air, want the snow fill", x, z)
			default:
				village++
			}
		}
	}
	if snow == 0 || village < 16 {
		t.Errorf("at the snow's y: %d snow and %d village blocks, want both, at least 16 village blocks", snow, village)
	}
}
