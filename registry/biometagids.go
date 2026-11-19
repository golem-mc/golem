package registry

// Typed IDs of the built-in biome tags the server tests, stable across reloads.
var (
	BiomeTagIDAllowsTropicalFishSpawnsAtAnyHeight = mustBiomeTag("minecraft:allows_tropical_fish_spawns_at_any_height")
	BiomeTagIDIsBadlands                          = mustBiomeTag("minecraft:is_badlands")
	BiomeTagIDIsJungle                            = mustBiomeTag("minecraft:is_jungle")
	BiomeTagIDIsSavanna                           = mustBiomeTag("minecraft:is_savanna")
	BiomeTagIDMineshaftBlocking                   = mustBiomeTag("minecraft:mineshaft_blocking")
	BiomeTagIDMoreFrequentDrownedSpawns           = mustBiomeTag("minecraft:more_frequent_drowned_spawns")
	BiomeTagIDPolarBearsSpawnOnAlternateBlocks    = mustBiomeTag("minecraft:polar_bears_spawn_on_alternate_blocks")
	BiomeTagIDProducesCoralsFromBonemeal          = mustBiomeTag("minecraft:produces_corals_from_bonemeal")
	BiomeTagIDReduceWaterAmbientSpawns            = mustBiomeTag("minecraft:reduce_water_ambient_spawns")
	BiomeTagIDRequiredOceanMonumentSurrounding    = mustBiomeTag("minecraft:required_ocean_monument_surrounding")
	BiomeTagIDSpawnsColdVariantFarmAnimals        = mustBiomeTag("minecraft:spawns_cold_variant_farm_animals")
	BiomeTagIDSpawnsColdVariantFrogs              = mustBiomeTag("minecraft:spawns_cold_variant_frogs")
	BiomeTagIDSpawnsCoralVariantZombieNautilus    = mustBiomeTag("minecraft:spawns_coral_variant_zombie_nautilus")
	BiomeTagIDSpawnsGoldRabbits                   = mustBiomeTag("minecraft:spawns_gold_rabbits")
	BiomeTagIDSpawnsSnowFoxes                     = mustBiomeTag("minecraft:spawns_snow_foxes")
	BiomeTagIDSpawnsWarmVariantFarmAnimals        = mustBiomeTag("minecraft:spawns_warm_variant_farm_animals")
	BiomeTagIDSpawnsWarmVariantFrogs              = mustBiomeTag("minecraft:spawns_warm_variant_frogs")
	BiomeTagIDSpawnsWhiteRabbits                  = mustBiomeTag("minecraft:spawns_white_rabbits")
	BiomeTagIDWithoutWanderingTraderSpawns        = mustBiomeTag("minecraft:without_wandering_trader_spawns")
	BiomeTagIDWithoutZombieSieges                 = mustBiomeTag("minecraft:without_zombie_sieges")
)

// mustBiomeTag rejects a misspelled built-in tag at initialization.
func mustBiomeTag(tag string) BiomeTagID {
	id := BiomeTagOf(tag)
	if id == noBiomeTag {
		panic("registry: unknown biome tag " + tag)
	}
	return id
}
