package world

import (
	"maps"
	"math/bits"
	"strconv"

	"github.com/golem-mc/golem/nbt"
)

// levelChunkFloor is the oldest DataVersion of a chunk in the pre-1.18 layout
// (a Level wrapper) that unwrapLevelChunk reads: 1.16. No block or block
// entity fix lies between it and 1.16.5 (2586); 1.15.2 and older chunks need
// the bit storage alignment and the 1.16 snapshot fixes, which are not ported.
// vanilla: DataFixers.addFixers (v2527 BitStorageAlignFix)
const levelChunkFloor = 2566

// Data versions of the block and item renames a 1.16.5 chunk gets that a
// 1.17 chunk is already past.
const (
	cauldronRenameVersion  = 2679 // vanilla: DataFixers.addFixers (CauldronRenameFix)
	grassPathRenameVersion = 2680 // vanilla: DataFixers.addFixers (Renamed grass path to dirt path)
)

// Section counts and offsets of the height change.
// vanilla: ChunkHeightAndBiomeFix
const (
	oldSectionCount = 16
	newSectionCount = 24
	newMinSectionY  = -4
	// topBiomeCells is the first cell of an old biome array's top layer.
	topBiomeCells = 1008
)

// levelRenames are the Level fields that keep their value under a new name.
// vanilla: ChunkRenamesFix, ChunkProtoTickListFix (LiquidTicks)
var levelRenames = [][2]string{
	{"Sections", "sections"}, {"TileEntities", "block_entities"}, {"TileTicks", "block_ticks"},
	{"LiquidTicks", "fluid_ticks"}, {"Entities", "entities"}, {"Structures", "structures"},
}

// levelProtoKeys are the Level fields of a chunk still being generated; a full
// chunk has no use for them and the new layout sizes them for 24 sections.
// vanilla: ChunkHeightAndBiomeFix.updateChunkTag
var levelProtoKeys = []string{"Biomes", "Heightmaps", "CarvingMasks", "LiquidsToBeTicked", "PostProcessing", "ToBeTicked", "Lights"}

// unwrapLevelChunk rewrites a chunk in the layout before 1.18 (everything
// under a Level compound, one block palette and packed array per section, a
// biome int array) in place to the 1.18 layout decodeChunkNBT reads. It
// reports an unsupportedVersionError for a chunk holding entities or a
// spawner, whose pre-1.20 formats are not upgraded.
//
// Not carried over: the blending data and below-zero retrogeneration vanilla
// adds for chunks of a noise world, which this server has no use for.
// vanilla: ChunkRenamesFix, ChunkHeightAndBiomeFix, ChunkProtoTickListFix
func unwrapLevelChunk(root nbt.Compound, version int32) error {
	level, ok := nbt.Get[nbt.Compound](root, "Level")
	if !ok {
		return nil
	}
	delete(root, "Level")
	maps.Copy(root, level)
	for _, r := range levelRenames {
		if v, ok := root[r[0]]; ok {
			delete(root, r[0])
			root[r[1]] = v
		}
	}
	if structures, ok := nbt.Get[nbt.Compound](root, "structures"); ok {
		if starts, ok := structures["Starts"]; ok {
			delete(structures, "Starts")
			structures["starts"] = starts
		}
	}
	if entities, ok := nbt.Get[nbt.List](root, "entities"); ok && len(entities.Items) > 0 {
		return &unsupportedVersionError{version: version}
	}
	if be, ok := nbt.Get[nbt.List](root, "block_entities"); ok {
		for _, item := range be.Items {
			entry, _ := nbt.CompoundView(item)
			if id, _ := nbt.Get[nbt.String](entry, "id"); id == "minecraft:mob_spawner" {
				return &unsupportedVersionError{version: version}
			}
		}
	}

	oldBiomes, _ := nbt.Get[nbt.IntArray](level, "Biomes")
	biomes := biomeContainers(oldBiomes)
	sections, _ := nbt.Get[nbt.List](root, "sections")
	done := map[int32]bool{}
	for _, item := range sections.Items {
		sec, ok := nbt.CompoundView(item)
		if !ok {
			continue
		}
		y, _ := nbt.Get[nbt.Byte](sec, "Y")
		done[int32(y)] = true
		if idx := int(y) - newMinSectionY; idx >= 0 && idx < len(biomes) && biomes[idx] != nil {
			sec["biomes"] = biomes[idx]
		}
		fixLevelSectionBlocks(sec)
	}
	for idx, container := range biomes {
		if y := int32(idx + newMinSectionY); container != nil && !done[y] {
			sections.Items = append(sections.Items, nbt.Compound{"Y": nbt.Byte(y), "biomes": container})
		}
	}
	if len(sections.Items) > 0 {
		sections.Elem = nbt.TypeCompound
		root["sections"] = sections
	}

	for _, key := range levelProtoKeys {
		delete(root, key)
	}
	if upgrade, ok := nbt.Get[nbt.Compound](root, "UpgradeData"); ok {
		shiftUpgradeIndices(upgrade)
	}
	return nil
}

// fixLevelSectionBlocks turns a section's Palette and BlockStates into the
// block_states compound; a section without both is air.
// vanilla: ChunkHeightAndBiomeFix.makeOptimizedPalettedContainer
func fixLevelSectionBlocks(sec nbt.Compound) {
	palette, hasPalette := nbt.Get[nbt.List](sec, "Palette")
	data, hasData := nbt.Get[nbt.LongArray](sec, "BlockStates")
	delete(sec, "Palette")
	delete(sec, "BlockStates")
	if !hasPalette || !hasData {
		return
	}
	bs := nbt.Compound{"palette": palette}
	if len(palette.Items) > 1 {
		// Data wider than the palette needs is a palette padded with air.
		if stored := len(data) * 64 / blocksPerSection; stored > bits.Len(uint(len(palette.Items)-1)) {
			air := nbt.Compound{"Name": nbt.String("minecraft:air")}
			for len(palette.Items) < 1<<(stored-1)+1 {
				palette.Items = append(palette.Items, air)
			}
			bs["palette"] = palette
		}
		bs["data"] = data
	}
	sec["block_states"] = bs
}

// biomeContainers builds the biome palette container of each of the 24
// sections from an old biome array: 1024 entries cover sections 0 to 15 and
// repeat their bottom and top layers beyond, 1536 cover all 24; anything else
// leaves every container nil, which reads as the default biome.
// vanilla: ChunkHeightAndBiomeFix.getBiomeContainers
func biomeContainers(old nbt.IntArray) []nbt.Compound {
	containers := make([]nbt.Compound, newSectionCount)
	switch len(old) {
	case newSectionCount * biomesPerSection:
		for i := range containers {
			containers[i] = biomeContainer(func(c int) int32 { return old[i*biomesPerSection+c] })
		}
	case oldSectionCount * biomesPerSection:
		for i := range oldSectionCount {
			containers[i-newMinSectionY] = biomeContainer(func(c int) int32 { return old[i*biomesPerSection+c] })
		}
		below := biomeContainer(func(c int) int32 { return old[c%16] })
		above := biomeContainer(func(c int) int32 { return old[c%16+topBiomeCells] })
		for i := range -newMinSectionY {
			containers[i] = below
			containers[newSectionCount-1-i] = above
		}
	}
	return containers
}

// biomeContainer packs the 64 biome cells of a section, palette in order of
// first use.
// vanilla: ChunkHeightAndBiomeFix.makeBiomeContainer
func biomeContainer(cell func(int) int32) nbt.Compound {
	index := map[int32]uint16{}
	var palette []nbt.Tag
	indices := make([]uint16, biomesPerSection)
	for c := range indices {
		id := cell(c) & 0xFF
		i, ok := index[id]
		if !ok {
			i = uint16(len(palette))
			index[id] = i
			palette = append(palette, nbt.String(oldBiomeName(id)))
		}
		indices[c] = i
	}
	container := nbt.Compound{"palette": nbt.List{Elem: nbt.TypeString, Items: palette}}
	if len(palette) > 1 {
		container["data"] = packLongs(indices, bits.Len(uint(len(palette)-1)))
	}
	return container
}

// oldBiomeName is the current name of a pre-1.18 numeric biome ID; an unknown
// ID is plains.
func oldBiomeName(id int32) string {
	name, ok := oldBiomeIDs[id]
	if !ok {
		return "minecraft:plains"
	}
	if to, ok := biomeRenames[name]; ok {
		return to
	}
	return name
}

// shiftUpgradeIndices moves the section indices of an UpgradeData up by the
// sections the height change added below.
// vanilla: ChunkHeightAndBiomeFix.shiftUpgradeData
func shiftUpgradeIndices(upgrade nbt.Compound) {
	indices, ok := nbt.Get[nbt.Compound](upgrade, "Indices")
	if !ok {
		return
	}
	shifted := nbt.Compound{}
	for key, v := range indices {
		if i, err := strconv.Atoi(key); err == nil {
			shifted[strconv.Itoa(i-newMinSectionY)] = v
		}
	}
	upgrade["Indices"] = shifted
}

// oldBiomeIDs are the numeric biome IDs of before 1.18 and their names then.
// vanilla: ChunkHeightAndBiomeFix.BIOMES_BY_ID
var oldBiomeIDs = map[int32]string{
	0:   "minecraft:ocean",
	1:   "minecraft:plains",
	2:   "minecraft:desert",
	3:   "minecraft:mountains",
	4:   "minecraft:forest",
	5:   "minecraft:taiga",
	6:   "minecraft:swamp",
	7:   "minecraft:river",
	8:   "minecraft:nether_wastes",
	9:   "minecraft:the_end",
	10:  "minecraft:frozen_ocean",
	11:  "minecraft:frozen_river",
	12:  "minecraft:snowy_tundra",
	13:  "minecraft:snowy_mountains",
	14:  "minecraft:mushroom_fields",
	15:  "minecraft:mushroom_field_shore",
	16:  "minecraft:beach",
	17:  "minecraft:desert_hills",
	18:  "minecraft:wooded_hills",
	19:  "minecraft:taiga_hills",
	20:  "minecraft:mountain_edge",
	21:  "minecraft:jungle",
	22:  "minecraft:jungle_hills",
	23:  "minecraft:jungle_edge",
	24:  "minecraft:deep_ocean",
	25:  "minecraft:stone_shore",
	26:  "minecraft:snowy_beach",
	27:  "minecraft:birch_forest",
	28:  "minecraft:birch_forest_hills",
	29:  "minecraft:dark_forest",
	30:  "minecraft:snowy_taiga",
	31:  "minecraft:snowy_taiga_hills",
	32:  "minecraft:giant_tree_taiga",
	33:  "minecraft:giant_tree_taiga_hills",
	34:  "minecraft:wooded_mountains",
	35:  "minecraft:savanna",
	36:  "minecraft:savanna_plateau",
	37:  "minecraft:badlands",
	38:  "minecraft:wooded_badlands_plateau",
	39:  "minecraft:badlands_plateau",
	40:  "minecraft:small_end_islands",
	41:  "minecraft:end_midlands",
	42:  "minecraft:end_highlands",
	43:  "minecraft:end_barrens",
	44:  "minecraft:warm_ocean",
	45:  "minecraft:lukewarm_ocean",
	46:  "minecraft:cold_ocean",
	47:  "minecraft:deep_warm_ocean",
	48:  "minecraft:deep_lukewarm_ocean",
	49:  "minecraft:deep_cold_ocean",
	50:  "minecraft:deep_frozen_ocean",
	127: "minecraft:the_void",
	129: "minecraft:sunflower_plains",
	130: "minecraft:desert_lakes",
	131: "minecraft:gravelly_mountains",
	132: "minecraft:flower_forest",
	133: "minecraft:taiga_mountains",
	134: "minecraft:swamp_hills",
	140: "minecraft:ice_spikes",
	149: "minecraft:modified_jungle",
	151: "minecraft:modified_jungle_edge",
	155: "minecraft:tall_birch_forest",
	156: "minecraft:tall_birch_hills",
	157: "minecraft:dark_forest_hills",
	158: "minecraft:snowy_taiga_mountains",
	160: "minecraft:giant_spruce_taiga",
	161: "minecraft:giant_spruce_taiga_hills",
	162: "minecraft:modified_gravelly_mountains",
	163: "minecraft:shattered_savanna",
	164: "minecraft:shattered_savanna_plateau",
	165: "minecraft:eroded_badlands",
	166: "minecraft:modified_wooded_badlands_plateau",
	167: "minecraft:modified_badlands_plateau",
	168: "minecraft:bamboo_jungle",
	169: "minecraft:bamboo_jungle_hills",
	170: "minecraft:soul_sand_valley",
	171: "minecraft:crimson_forest",
	172: "minecraft:warped_forest",
	173: "minecraft:basalt_deltas",
	174: "minecraft:dripstone_caves",
	175: "minecraft:lush_caves",
	177: "minecraft:meadow",
	178: "minecraft:grove",
	179: "minecraft:snowy_slopes",
	180: "minecraft:snowcapped_peaks",
	181: "minecraft:lofty_peaks",
	182: "minecraft:stony_peaks",
}

// biomeRenames are the biome renames of 1.18, and of the removed deep warm ocean.
// vanilla: CavesAndCliffsRenames.RENAMES, DataFixers.addFixers (v2843)
var biomeRenames = map[string]string{
	"minecraft:badlands_plateau":                 "minecraft:badlands",
	"minecraft:bamboo_jungle_hills":              "minecraft:bamboo_jungle",
	"minecraft:birch_forest_hills":               "minecraft:birch_forest",
	"minecraft:dark_forest_hills":                "minecraft:dark_forest",
	"minecraft:desert_hills":                     "minecraft:desert",
	"minecraft:desert_lakes":                     "minecraft:desert",
	"minecraft:giant_spruce_taiga_hills":         "minecraft:old_growth_spruce_taiga",
	"minecraft:giant_spruce_taiga":               "minecraft:old_growth_spruce_taiga",
	"minecraft:giant_tree_taiga_hills":           "minecraft:old_growth_pine_taiga",
	"minecraft:giant_tree_taiga":                 "minecraft:old_growth_pine_taiga",
	"minecraft:gravelly_mountains":               "minecraft:windswept_gravelly_hills",
	"minecraft:jungle_edge":                      "minecraft:sparse_jungle",
	"minecraft:jungle_hills":                     "minecraft:jungle",
	"minecraft:modified_badlands_plateau":        "minecraft:badlands",
	"minecraft:modified_gravelly_mountains":      "minecraft:windswept_gravelly_hills",
	"minecraft:modified_jungle_edge":             "minecraft:sparse_jungle",
	"minecraft:modified_jungle":                  "minecraft:jungle",
	"minecraft:modified_wooded_badlands_plateau": "minecraft:wooded_badlands",
	"minecraft:mountain_edge":                    "minecraft:windswept_hills",
	"minecraft:mountains":                        "minecraft:windswept_hills",
	"minecraft:mushroom_field_shore":             "minecraft:mushroom_fields",
	"minecraft:shattered_savanna":                "minecraft:windswept_savanna",
	"minecraft:shattered_savanna_plateau":        "minecraft:windswept_savanna",
	"minecraft:snowy_mountains":                  "minecraft:snowy_plains",
	"minecraft:snowy_taiga_hills":                "minecraft:snowy_taiga",
	"minecraft:snowy_taiga_mountains":            "minecraft:snowy_taiga",
	"minecraft:snowy_tundra":                     "minecraft:snowy_plains",
	"minecraft:stone_shore":                      "minecraft:stony_shore",
	"minecraft:swamp_hills":                      "minecraft:swamp",
	"minecraft:taiga_hills":                      "minecraft:taiga",
	"minecraft:taiga_mountains":                  "minecraft:taiga",
	"minecraft:tall_birch_forest":                "minecraft:old_growth_birch_forest",
	"minecraft:tall_birch_hills":                 "minecraft:old_growth_birch_forest",
	"minecraft:wooded_badlands_plateau":          "minecraft:wooded_badlands",
	"minecraft:wooded_hills":                     "minecraft:forest",
	"minecraft:wooded_mountains":                 "minecraft:windswept_forest",
	"minecraft:lofty_peaks":                      "minecraft:jagged_peaks",
	"minecraft:snowcapped_peaks":                 "minecraft:frozen_peaks",
	"minecraft:deep_warm_ocean":                  "minecraft:warm_ocean",
}
