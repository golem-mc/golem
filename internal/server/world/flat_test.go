package world

import (
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
