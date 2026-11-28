package advancement

import "github.com/golem-mc/golem/internal/server/world"

// Renames the vanilla data fixers apply to a progress file or a recipe book
// saved by an older version. Each step runs on a file whose DataVersion is
// below its own.

// DefaultDataVersion is the version of a file without a DataVersion.
// vanilla: DataFixTypes.updateToCurrentVersion's default
const DefaultDataVersion = 1343

const catalogue = "minecraft:husbandry/complete_catalogue"

type renameStep struct {
	version int
	// ids renames advancement ids; recipes renames recipe ids.
	ids, recipes map[string]string
	// criteria renames one advancement's criteria.
	criteria map[string]map[string]string
}

// renameSteps are in version order.
var renameSteps = []renameStep{
	// vanilla: DataFixers v2100 "Rename sugar recipe"
	{version: 2100, recipes: map[string]string{"minecraft:sugar": "minecraft:sugar_from_sugar_cane"},
		ids: map[string]string{"minecraft:recipes/misc/sugar": "minecraft:recipes/misc/sugar_from_sugar_cane"}},
	// vanilla: DataFixers v2503 "Composter category change"
	{version: 2503, ids: map[string]string{"minecraft:recipes/misc/composter": "minecraft:recipes/decorations/composter"}},
	// vanilla: DataFixers v2846 "Rename some C&C part 2 advancements"
	{version: 2846, ids: map[string]string{
		"minecraft:husbandry/play_jukebox_in_meadows":        "minecraft:adventure/play_jukebox_in_meadows",
		"minecraft:adventure/caves_and_cliff":                "minecraft:adventure/fall_from_world_height",
		"minecraft:adventure/ride_strider_in_overworld_lava": "minecraft:nether/ride_strider_in_overworld_lava",
	}},
	// vanilla: DataFixers v3086 "Migrate cat variant advancement"
	{version: 3086, criteria: map[string]map[string]string{catalogue: {
		"textures/entity/cat/tabby.png":             "minecraft:tabby",
		"textures/entity/cat/black.png":             "minecraft:black",
		"textures/entity/cat/red.png":               "minecraft:red",
		"textures/entity/cat/siamese.png":           "minecraft:siamese",
		"textures/entity/cat/british_shorthair.png": "minecraft:british",
		"textures/entity/cat/calico.png":            "minecraft:calico",
		"textures/entity/cat/persian.png":           "minecraft:persian",
		"textures/entity/cat/ragdoll.png":           "minecraft:ragdoll",
		"textures/entity/cat/white.png":             "minecraft:white",
		"textures/entity/cat/jellie.png":            "minecraft:jellie",
		"textures/entity/cat/all_black.png":         "minecraft:all_black",
	}}},
	// vanilla: DataFixers v3097 "Migrate cat variant advancement for british shorthair"
	{version: 3097, criteria: map[string]map[string]string{catalogue: {"minecraft:british": "minecraft:british_shorthair"}}},
	// vanilla: DataFixers v4311 "Use lodestone category change"
	{version: 4311, ids: map[string]string{"minecraft:nether/use_lodestone": "minecraft:adventure/use_lodestone"}},
	// vanilla: DataFixers v4769, StonecutterRecipeRenameFix
	{version: 4769, recipes: map[string]string{
		"minecraft:chiseled_stone_bricks_stone_from_stonecutting":                "minecraft:chiseled_stone_bricks_from_stone_stonecutting",
		"minecraft:end_stone_brick_slab_from_end_stone_brick_stonecutting":       "minecraft:end_stone_brick_slab_from_end_stone_bricks_stonecutting",
		"minecraft:end_stone_brick_stairs_from_end_stone_brick_stonecutting":     "minecraft:end_stone_brick_stairs_from_end_stone_bricks_stonecutting",
		"minecraft:end_stone_brick_wall_from_end_stone_brick_stonecutting":       "minecraft:end_stone_brick_wall_from_end_stone_bricks_stonecutting",
		"minecraft:mossy_stone_brick_slab_from_mossy_stone_brick_stonecutting":   "minecraft:mossy_stone_brick_slab_from_mossy_stone_bricks_stonecutting",
		"minecraft:mossy_stone_brick_stairs_from_mossy_stone_brick_stonecutting": "minecraft:mossy_stone_brick_stairs_from_mossy_stone_bricks_stonecutting",
		"minecraft:mossy_stone_brick_wall_from_mossy_stone_brick_stonecutting":   "minecraft:mossy_stone_brick_wall_from_mossy_stone_bricks_stonecutting",
		"minecraft:prismarine_brick_slab_from_prismarine_stonecutting":           "minecraft:prismarine_brick_slab_from_prismarine_bricks_stonecutting",
		"minecraft:prismarine_brick_stairs_from_prismarine_stonecutting":         "minecraft:prismarine_brick_stairs_from_prismarine_bricks_stonecutting",
		"minecraft:quartz_slab_from_stonecutting":                                "minecraft:quartz_slab_from_quartz_block_stonecutting",
		"minecraft:stone_brick_walls_from_stone_stonecutting":                    "minecraft:stone_brick_wall_from_stone_stonecutting",
	}, ids: map[string]string{
		"minecraft:recipes/building_blocks/chiseled_stone_bricks_stone_from_stonecutting":                "minecraft:recipes/building_blocks/chiseled_stone_bricks_from_stone_stonecutting",
		"minecraft:recipes/building_blocks/end_stone_brick_slab_from_end_stone_brick_stonecutting":       "minecraft:recipes/building_blocks/end_stone_brick_slab_from_end_stone_bricks_stonecutting",
		"minecraft:recipes/building_blocks/end_stone_brick_stairs_from_end_stone_brick_stonecutting":     "minecraft:recipes/building_blocks/end_stone_brick_stairs_from_end_stone_bricks_stonecutting",
		"minecraft:recipes/decorations/end_stone_brick_wall_from_end_stone_brick_stonecutting":           "minecraft:recipes/decorations/end_stone_brick_wall_from_end_stone_bricks_stonecutting",
		"minecraft:recipes/building_blocks/mossy_stone_brick_slab_from_mossy_stone_brick_stonecutting":   "minecraft:recipes/building_blocks/mossy_stone_brick_slab_from_mossy_stone_bricks_stonecutting",
		"minecraft:recipes/building_blocks/mossy_stone_brick_stairs_from_mossy_stone_brick_stonecutting": "minecraft:recipes/building_blocks/mossy_stone_brick_stairs_from_mossy_stone_bricks_stonecutting",
		"minecraft:recipes/building_blocks/mossy_stone_brick_wall_from_mossy_stone_brick_stonecutting":   "minecraft:recipes/decorations/mossy_stone_brick_wall_from_mossy_stone_bricks_stonecutting",
		"minecraft:recipes/building_blocks/prismarine_brick_slab_from_prismarine_stonecutting":           "minecraft:recipes/building_blocks/prismarine_brick_slab_from_prismarine_bricks_stonecutting",
		"minecraft:recipes/building_blocks/prismarine_brick_stairs_from_prismarine_stonecutting":         "minecraft:recipes/building_blocks/prismarine_brick_stairs_from_prismarine_bricks_stonecutting",
		"minecraft:recipes/building_blocks/quartz_slab_from_stonecutting":                                "minecraft:recipes/building_blocks/quartz_slab_from_quartz_block_stonecutting",
		"minecraft:recipes/decorations/stone_brick_walls_from_stone_stonecutting":                        "minecraft:recipes/decorations/stone_brick_wall_from_stone_stonecutting",
	}},
}

// criterionRenamers rename the criteria of the advancements whose criteria
// are biome or entity names, by the advancement's current id.
// vanilla: DataFixers (ADVANCEMENTS schema, V1460)
var criterionRenamers = map[string]func(string, int32) string{
	"minecraft:adventure/adventuring_time": world.RenameOldBiome,
	"minecraft:adventure/kill_a_mob":       world.RenameOldEntity,
	"minecraft:adventure/kill_all_mobs":    world.RenameOldEntity,
	"minecraft:husbandry/bred_all_animals": world.RenameOldEntity,
}

// RenameAdvancementID returns id as the current version names it.
func RenameAdvancementID(id string, fileVersion int) string {
	for _, s := range renameSteps {
		if s.version > fileVersion {
			if to, ok := s.ids[id]; ok {
				id = to
			}
		}
	}
	return id
}

// RenameCriterion returns an advancement's criterion as the current version
// names it. id is the current name of the advancement.
func RenameCriterion(id, name string, fileVersion int) string {
	if rename := criterionRenamers[id]; rename != nil {
		name = rename(name, int32(fileVersion))
	}
	for _, s := range renameSteps {
		if s.version > fileVersion {
			if to, ok := s.criteria[id][name]; ok {
				name = to
			}
		}
	}
	return name
}

// RenameRecipe returns a recipe id as the current version names it.
func RenameRecipe(id string, fileVersion int) string {
	for _, s := range renameSteps {
		if s.version > fileVersion {
			if to, ok := s.recipes[id]; ok {
				id = to
			}
		}
	}
	return id
}
