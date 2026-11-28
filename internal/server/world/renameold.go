package world

import "github.com/golem-mc/golem/registry"

// Data versions of the renames in nameSteps that no chunk fix names.
const (
	beehiveVersion     = 2209 // vanilla: DataFixers.addFixers (Rename bee_hive to beehive)
	copperVersion      = 2690 // vanilla: DataFixers.addFixers (copper oxidized terms)
	copperSuffixVer    = 2691 // vanilla: DataFixers.addFixers (copper suffixes)
	deepslateVersion   = 2696 // vanilla: DataFixers.addFixers (grimstone to deepslate)
	caveVinesVersion   = 2700 // vanilla: DataFixers.addFixers (cave vines blocks)
	azaleaVersion      = 2717 // vanilla: DataFixers.addFixers (azalea_leaves_flowers)
	nameBiomeNether    = 2552 // vanilla: DataFixers.addFixers (Nether biome rename)
	biomeRenamesCaves  = 2838 // vanilla: DataFixers.addFixers (Caves and Cliffs biome renames)
	deepWarmOceanFixed = 2843 // vanilla: DataFixers.addFixers (Remove Deep Warm Ocean)
)

// nameStep is the renames one data fixer makes to item and block names.
type nameStep struct {
	version       int32
	items, blocks map[string]string
}

// both returns m as the renames of items and of blocks alike.
func both(m map[string]string) nameStep { return nameStep{items: m, blocks: m} }

func withVersion(v int32, s nameStep) nameStep { s.version = v; return s }

// nameSteps are the item and block renames since 1.15, in version order, that
// stats and advancement files carry (a chunk or stack has its own fixes in
// chunknbt.go and renameOldItem for the same data).
// vanilla: DataFixers.addFixers (ItemRenameFix, LegacyBlockRenameFix)
var nameSteps = []nameStep{
	withVersion(beehiveVersion, both(map[string]string{"minecraft:bee_hive": "minecraft:beehive"})),
	withVersion(fungusRenameVersion, both(map[string]string{
		"minecraft:warped_fungi": "minecraft:warped_fungus", "minecraft:crimson_fungi": "minecraft:crimson_fungus"})),
	withVersion(zombifiedPiglinVersion, nameStep{items: map[string]string{
		"minecraft:zombie_pigman_spawn_egg": "minecraft:zombified_piglin_spawn_egg"}}),
	withVersion(soulFireRenameVersion, both(map[string]string{
		"minecraft:soul_fire_torch": "minecraft:soul_torch", "minecraft:soul_fire_wall_torch": "minecraft:soul_wall_torch",
		"minecraft:soul_fire_lantern": "minecraft:soul_lantern"})),
	withVersion(grassPathRenameVersion, both(map[string]string{"minecraft:grass_path": "minecraft:dirt_path"})),
	withVersion(copperVersion, both(map[string]string{
		"minecraft:weathered_copper_block":                    "minecraft:oxidized_copper_block",
		"minecraft:semi_weathered_copper_block":               "minecraft:weathered_copper_block",
		"minecraft:lightly_weathered_copper_block":            "minecraft:exposed_copper_block",
		"minecraft:weathered_cut_copper":                      "minecraft:oxidized_cut_copper",
		"minecraft:semi_weathered_cut_copper":                 "minecraft:weathered_cut_copper",
		"minecraft:lightly_weathered_cut_copper":              "minecraft:exposed_cut_copper",
		"minecraft:weathered_cut_copper_stairs":               "minecraft:oxidized_cut_copper_stairs",
		"minecraft:semi_weathered_cut_copper_stairs":          "minecraft:weathered_cut_copper_stairs",
		"minecraft:lightly_weathered_cut_copper_stairs":       "minecraft:exposed_cut_copper_stairs",
		"minecraft:weathered_cut_copper_slab":                 "minecraft:oxidized_cut_copper_slab",
		"minecraft:semi_weathered_cut_copper_slab":            "minecraft:weathered_cut_copper_slab",
		"minecraft:lightly_weathered_cut_copper_slab":         "minecraft:exposed_cut_copper_slab",
		"minecraft:waxed_semi_weathered_copper":               "minecraft:waxed_weathered_copper",
		"minecraft:waxed_lightly_weathered_copper":            "minecraft:waxed_exposed_copper",
		"minecraft:waxed_semi_weathered_cut_copper":           "minecraft:waxed_weathered_cut_copper",
		"minecraft:waxed_lightly_weathered_cut_copper":        "minecraft:waxed_exposed_cut_copper",
		"minecraft:waxed_semi_weathered_cut_copper_stairs":    "minecraft:waxed_weathered_cut_copper_stairs",
		"minecraft:waxed_lightly_weathered_cut_copper_stairs": "minecraft:waxed_exposed_cut_copper_stairs",
		"minecraft:waxed_semi_weathered_cut_copper_slab":      "minecraft:waxed_weathered_cut_copper_slab",
		"minecraft:waxed_lightly_weathered_cut_copper_slab":   "minecraft:waxed_exposed_cut_copper_slab",
	})),
	withVersion(copperSuffixVer, both(map[string]string{
		"minecraft:waxed_copper":           "minecraft:waxed_copper_block",
		"minecraft:oxidized_copper_block":  "minecraft:oxidized_copper",
		"minecraft:weathered_copper_block": "minecraft:weathered_copper",
		"minecraft:exposed_copper_block":   "minecraft:exposed_copper",
	})),
	withVersion(deepslateVersion, both(map[string]string{
		"minecraft:grimstone":                 "minecraft:deepslate",
		"minecraft:grimstone_slab":            "minecraft:cobbled_deepslate_slab",
		"minecraft:grimstone_stairs":          "minecraft:cobbled_deepslate_stairs",
		"minecraft:grimstone_wall":            "minecraft:cobbled_deepslate_wall",
		"minecraft:polished_grimstone":        "minecraft:polished_deepslate",
		"minecraft:polished_grimstone_slab":   "minecraft:polished_deepslate_slab",
		"minecraft:polished_grimstone_stairs": "minecraft:polished_deepslate_stairs",
		"minecraft:polished_grimstone_wall":   "minecraft:polished_deepslate_wall",
		"minecraft:grimstone_tiles":           "minecraft:deepslate_tiles",
		"minecraft:grimstone_tile_slab":       "minecraft:deepslate_tile_slab",
		"minecraft:grimstone_tile_stairs":     "minecraft:deepslate_tile_stairs",
		"minecraft:grimstone_tile_wall":       "minecraft:deepslate_tile_wall",
		"minecraft:grimstone_bricks":          "minecraft:deepslate_bricks",
		"minecraft:grimstone_brick_slab":      "minecraft:deepslate_brick_slab",
		"minecraft:grimstone_brick_stairs":    "minecraft:deepslate_brick_stairs",
		"minecraft:grimstone_brick_wall":      "minecraft:deepslate_brick_wall",
		"minecraft:chiseled_grimstone":        "minecraft:chiseled_deepslate",
	})),
	withVersion(caveVinesVersion, nameStep{blocks: map[string]string{
		"minecraft:cave_vines_head": "minecraft:cave_vines", "minecraft:cave_vines_body": "minecraft:cave_vines_plant"}}),
	withVersion(azaleaVersion, both(map[string]string{"minecraft:azalea_leaves_flowers": "minecraft:flowering_azalea_leaves"})),
	withVersion(grassRenameVersion, both(map[string]string{"minecraft:grass": "minecraft:short_grass"})),
	withVersion(scuteRenameVersion, nameStep{items: map[string]string{"minecraft:scute": "minecraft:turtle_scute"}}),
	withVersion(ironChainVersion, both(map[string]string{"minecraft:chain": "minecraft:iron_chain"})),
	withVersion(explorerMapRenameVersion, nameStep{items: templateExplorerMapRenames}),
}

// RenameOldItem returns an item name as the current version names it, given
// the DataVersion of the data it was read from.
func RenameOldItem(name string, version int32) string {
	return renameOldName(name, version, func(s nameStep) map[string]string { return s.items })
}

// RenameOldBlock returns a block name as the current version names it, given
// the DataVersion of the data it was read from.
func RenameOldBlock(name string, version int32) string {
	return renameOldName(name, version, func(s nameStep) map[string]string { return s.blocks })
}

// renameOldName applies, in version order, every step newer than version.
func renameOldName(name string, version int32, pick func(nameStep) map[string]string) string {
	cur := registry.Qualify(name)
	for _, s := range nameSteps {
		if version < s.version {
			if to, ok := pick(s)[cur]; ok {
				cur = to
			}
		}
	}
	if cur == registry.Qualify(name) {
		return name
	}
	return cur
}

// RenameOldBiome returns a biome name as the current version names it.
// vanilla: DataFixers.addFixers (v2552, v2838, v2843)
func RenameOldBiome(name string, version int32) string {
	name = registry.Qualify(name)
	if version < nameBiomeNether && name == "minecraft:nether" {
		name = "minecraft:nether_wastes"
	}
	if version < biomeRenamesCaves {
		if to, ok := biomeRenames[name]; ok {
			name = to
		}
	}
	if version < deepWarmOceanFixed && name == "minecraft:deep_warm_ocean" {
		name = "minecraft:warm_ocean"
	}
	return name
}

// RenameOldEntity returns an entity type name as the current version names it.
// vanilla: EntityZombifiedPiglinRenameFix
func RenameOldEntity(name string, version int32) string {
	name = registry.Qualify(name)
	if version < zombifiedPiglinVersion && name == "minecraft:zombie_pigman" {
		return "minecraft:zombified_piglin"
	}
	return name
}
