package rules

// Spawn rules: vanilla's decisions when a mob appears, as plain functions of
// place and dice.

import (
	"github.com/golem-mc/golem/registry"
)

// sheepPalette is a sheep spawn palette: the common colour, and four
// leftover dyes in weight order 5, 5, 5, 3.
type sheepPalette struct {
	common string
	others [4]string
}

// IntDice is the integer draw of a spawn's random: entity.Dice, or a
// levelgen.RandomSource during chunk generation.
type IntDice interface {
	// NextIntN returns a value in [0, n); n must be positive.
	NextIntN(n int32) int32
}

// SheepSpawnColour draws a newborn sheep's colour: warm palette for
// spawns_warm_variant_farm_animals biomes, cold for
// spawns_cold_variant_farm_animals, else (unknown biomes too) temperate.
// One draw of 100 picks the four leftovers at weights 5, 5, 5, 3 in list
// order, else the common branch, where a second draw of 500 makes 1 pink.
// vanilla: SheepColorSpawnRules.getSheepColor, WeightedList.getRandomOrThrow
func SheepSpawnColour(biome registry.Biome, rng IntDice) string {
	var palette sheepPalette
	switch {
	case biome.InTag(registry.BiomeTagIDSpawnsWarmVariantFarmAnimals):
		palette = sheepPalette{common: "brown", others: [4]string{"gray", "light_gray", "white", "black"}}
	case biome.InTag(registry.BiomeTagIDSpawnsColdVariantFarmAnimals):
		palette = sheepPalette{common: "black", others: [4]string{"light_gray", "gray", "white", "brown"}}
	default:
		palette = sheepPalette{common: "white", others: [4]string{"black", "gray", "light_gray", "brown"}}
	}
	roll := rng.NextIntN(100)
	for i, weight := range [4]int32{5, 5, 5, 3} {
		if roll < weight {
			return palette.others[i]
		}
		roll -= weight
	}
	if rng.NextIntN(500) < 499 {
		return palette.common
	}
	return "pink"
}

// FarmAnimalVariant picks a newborn cow, pig or chicken's variant from their
// shared spawn conditions: warm and cold at priority 1 in their tag,
// temperate at 0 anywhere, uniform among the top-priority matches.
// vanilla: VariantUtils.selectVariantToSpawn
func FarmAnimalVariant(biome registry.Biome, rng IntDice) string {
	var matched []string
	if biome.InTag(registry.BiomeTagIDSpawnsWarmVariantFarmAnimals) {
		matched = append(matched, "minecraft:warm")
	}
	if biome.InTag(registry.BiomeTagIDSpawnsColdVariantFarmAnimals) {
		matched = append(matched, "minecraft:cold")
	}
	if len(matched) == 0 {
		matched = append(matched, "minecraft:temperate")
	}
	return matched[rng.NextIntN(int32(len(matched)))]
}

// RandomRegistryEntry draws one entry of a registry uniformly; "" for an
// unknown registry.
// vanilla: VariantUtils.pickRandomSoundVariant
func RandomRegistryEntry(name string, rng IntDice) string {
	r, ok := registry.Get(name)
	if !ok || len(r.Entries) == 0 {
		return ""
	}
	return r.Entries[rng.NextIntN(int32(len(r.Entries)))]
}

// wolfBiomeVariants are the wolf variants whose spawn condition is a biome
// or a biome tag at priority 1; the pale wolf is the priority-0 fallback.
// vanilla: WolfVariants.bootstrap
var wolfBiomeVariants = []struct {
	variant string
	biome   registry.Biome
	tag     registry.BiomeTagID // the condition when biome is NoBiome
}{
	{"minecraft:spotted", registry.NoBiome, registry.BiomeTagIDIsSavanna},
	{"minecraft:snowy", registry.BiomeGrove, 0},
	{"minecraft:black", registry.BiomeOldGrowthPineTaiga, 0},
	{"minecraft:ashen", registry.BiomeSnowyTaiga, 0},
	{"minecraft:rusty", registry.NoBiome, registry.BiomeTagIDIsJungle},
	{"minecraft:woods", registry.BiomeForest, 0},
	{"minecraft:chestnut", registry.BiomeOldGrowthSpruceTaiga, 0},
	{"minecraft:striped", registry.NoBiome, registry.BiomeTagIDIsBadlands},
}

// WolfVariant picks the variant a wolf spawned in the biome takes: a uniform
// draw among the matches of the highest priority, the pale wolf where no
// biome matches.
// vanilla: VariantUtils.selectVariantToSpawn, WolfVariants.bootstrap
func WolfVariant(biome registry.Biome, rng IntDice) string {
	var matched []string
	for _, v := range wolfBiomeVariants {
		if (v.biome != registry.NoBiome && v.biome == biome) || (v.biome == registry.NoBiome && biome.InTag(v.tag)) {
			matched = append(matched, v.variant)
		}
	}
	if len(matched) == 0 {
		matched = append(matched, "minecraft:pale")
	}
	return matched[rng.NextIntN(int32(len(matched)))]
}
