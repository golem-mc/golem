package levelgen

// FeatureSlot is where a placed feature sits in a dimension's decoration:
// its step and its index among that step's features, which together seed
// its random. vanilla: FeatureSorter.buildFeaturesPerStep, WorldgenRandom.setFeatureSeed
type FeatureSlot struct{ Step, Index int32 }

// EndFeatureSlots are the End's placed features as FeatureSorter orders the
// features of its five biomes, taken in TheEndBiomeSource's order.
var EndFeatureSlots = map[string]FeatureSlot{
	"minecraft:end_island_decorated": {0, 0},
	"minecraft:end_gateway_return":   {4, 0},
	"minecraft:end_spike":            {4, 1},
	"minecraft:chorus_plant":         {9, 0},
	"minecraft:end_platform":         {10, 0},
}

// NetherFeature is a placed feature of the Nether's decoration with the
// biomes whose feature lists carry it, as a bit set indexed by NetherBiome.
type NetherFeature struct {
	Name   string
	Slot   FeatureSlot
	Biomes uint8
}

// NetherFeatures are the placed features FeatureSorter orders from the
// feature lists of the Nether's five biomes, taken in the parameter list's
// order, by step and then index. The table is derived from the biome
// definitions in the data pack.
// vanilla: FeatureSorter.buildFeaturesPerStep, MultiNoiseBiomeSource
var NetherFeatures = []NetherFeature{
	{"minecraft:basalt_pillar", FeatureSlot{2, 0}, 1 << SoulSandValley},
	{"minecraft:delta", FeatureSlot{4, 0}, 1 << BasaltDeltas},
	{"minecraft:small_basalt_columns", FeatureSlot{4, 1}, 1 << BasaltDeltas},
	{"minecraft:large_basalt_columns", FeatureSlot{4, 2}, 1 << BasaltDeltas},
	{"minecraft:basalt_blobs", FeatureSlot{7, 0}, 1 << BasaltDeltas},
	{"minecraft:blackstone_blobs", FeatureSlot{7, 1}, 1 << BasaltDeltas},
	{"minecraft:spring_delta", FeatureSlot{7, 2}, 1 << BasaltDeltas},
	{"minecraft:spring_open", FeatureSlot{7, 3}, 1<<NetherWastes | 1<<SoulSandValley | 1<<CrimsonForest | 1<<WarpedForest},
	{"minecraft:patch_fire", FeatureSlot{7, 4}, 1<<NetherWastes | 1<<SoulSandValley | 1<<CrimsonForest | 1<<WarpedForest | 1<<BasaltDeltas},
	{"minecraft:patch_soul_fire", FeatureSlot{7, 5}, 1<<NetherWastes | 1<<SoulSandValley | 1<<WarpedForest | 1<<BasaltDeltas},
	{"minecraft:glowstone_extra", FeatureSlot{7, 6}, 1<<NetherWastes | 1<<SoulSandValley | 1<<CrimsonForest | 1<<WarpedForest | 1<<BasaltDeltas},
	{"minecraft:glowstone", FeatureSlot{7, 7}, 1<<NetherWastes | 1<<SoulSandValley | 1<<CrimsonForest | 1<<WarpedForest | 1<<BasaltDeltas},
	{"minecraft:patch_crimson_roots", FeatureSlot{7, 8}, 1 << SoulSandValley},
	{"minecraft:brown_mushroom_nether", FeatureSlot{7, 9}, 1<<NetherWastes | 1<<BasaltDeltas},
	{"minecraft:red_mushroom_nether", FeatureSlot{7, 10}, 1<<NetherWastes | 1<<BasaltDeltas},
	{"minecraft:ore_magma", FeatureSlot{7, 11}, 1<<NetherWastes | 1<<SoulSandValley | 1<<CrimsonForest | 1<<WarpedForest | 1<<BasaltDeltas},
	{"minecraft:spring_closed_double", FeatureSlot{7, 12}, 1 << BasaltDeltas},
	{"minecraft:ore_gold_deltas", FeatureSlot{7, 13}, 1 << BasaltDeltas},
	{"minecraft:ore_quartz_deltas", FeatureSlot{7, 14}, 1 << BasaltDeltas},
	{"minecraft:spring_closed", FeatureSlot{7, 15}, 1<<NetherWastes | 1<<SoulSandValley | 1<<CrimsonForest | 1<<WarpedForest},
	{"minecraft:ore_soul_sand", FeatureSlot{7, 16}, 1 << SoulSandValley},
	{"minecraft:ore_gravel_nether", FeatureSlot{7, 17}, 1<<NetherWastes | 1<<SoulSandValley | 1<<CrimsonForest | 1<<WarpedForest},
	{"minecraft:ore_blackstone", FeatureSlot{7, 18}, 1<<NetherWastes | 1<<SoulSandValley | 1<<CrimsonForest | 1<<WarpedForest},
	{"minecraft:ore_gold_nether", FeatureSlot{7, 19}, 1<<NetherWastes | 1<<SoulSandValley | 1<<CrimsonForest | 1<<WarpedForest},
	{"minecraft:ore_quartz_nether", FeatureSlot{7, 20}, 1<<NetherWastes | 1<<SoulSandValley | 1<<CrimsonForest | 1<<WarpedForest},
	{"minecraft:ore_ancient_debris_large", FeatureSlot{7, 21}, 1<<NetherWastes | 1<<SoulSandValley | 1<<CrimsonForest | 1<<WarpedForest | 1<<BasaltDeltas},
	{"minecraft:ore_debris_small", FeatureSlot{7, 22}, 1<<NetherWastes | 1<<SoulSandValley | 1<<CrimsonForest | 1<<WarpedForest | 1<<BasaltDeltas},
	{"minecraft:spring_lava", FeatureSlot{9, 0}, 1<<NetherWastes | 1<<SoulSandValley | 1<<CrimsonForest | 1<<WarpedForest},
	{"minecraft:brown_mushroom_normal", FeatureSlot{9, 1}, 1<<NetherWastes | 1<<CrimsonForest | 1<<WarpedForest},
	{"minecraft:red_mushroom_normal", FeatureSlot{9, 2}, 1<<NetherWastes | 1<<CrimsonForest | 1<<WarpedForest},
	{"minecraft:warped_fungi", FeatureSlot{9, 3}, 1 << WarpedForest},
	{"minecraft:warped_forest_vegetation", FeatureSlot{9, 4}, 1 << WarpedForest},
	{"minecraft:nether_sprouts", FeatureSlot{9, 5}, 1 << WarpedForest},
	{"minecraft:twisting_vines", FeatureSlot{9, 6}, 1 << WarpedForest},
	{"minecraft:weeping_vines", FeatureSlot{9, 7}, 1 << CrimsonForest},
	{"minecraft:crimson_fungi", FeatureSlot{9, 8}, 1 << CrimsonForest},
	{"minecraft:crimson_forest_vegetation", FeatureSlot{9, 9}, 1 << CrimsonForest},
}

// EndBiome is one of the End's five biomes.
type EndBiome uint8

// The End's biomes, in TheEndBiomeSource's order.
const (
	TheEnd EndBiome = iota
	EndHighlands
	EndMidlands
	SmallEndIslands
	EndBarrens
)
