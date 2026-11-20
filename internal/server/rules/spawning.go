package rules

// Spawn rules: vanilla's decisions when a mob appears, as plain functions of
// place and dice.

import (
	"math"
	"slices"

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

// SpawnContext is the world state at a mob's spawn position that variant
// spawn conditions inspect.
// vanilla: SpawnContext
type SpawnContext struct {
	Biome          registry.Biome
	MoonBrightness float32
	// InStructure reports whether the spawn position is inside a piece of the
	// named structure; nil means no structure.
	// vanilla: StructureManager.getStructureWithPieceAt
	InStructure func(structureID string) bool
}

// SelectVariantToSpawn chooses a variant entry from the named registry using
// each entry's spawn_conditions, drawing uniformly among the matches of the
// highest priority. Returns ("", false) without drawing when nothing matches.
// vanilla: VariantUtils.selectVariantToSpawn, PriorityProvider.pick
func SelectVariantToSpawn(registryName string, ctx SpawnContext, rng IntDice) (string, bool) {
	entries := registry.VariantSpawns(registryName)
	var matched []string
	best := math.MinInt
	for _, entry := range entries {
		for _, sel := range entry.Selectors {
			if sel.Priority < best {
				continue
			}
			if !matchesSpawnCondition(sel, ctx) {
				continue
			}
			if sel.Priority > best {
				best = sel.Priority
				matched = matched[:0]
			}
			matched = append(matched, entry.Name)
		}
	}
	if len(matched) == 0 {
		return "", false
	}
	return matched[rng.NextIntN(int32(len(matched)))], true
}

func matchesSpawnCondition(sel registry.VariantSpawnSelector, ctx SpawnContext) bool {
	switch sel.Kind {
	case registry.SpawnConditionAlways:
		return true
	case registry.SpawnConditionBiome:
		return sel.MatchesBiome(ctx.Biome)
	case registry.SpawnConditionMoonBrightness:
		moon := float64(ctx.MoonBrightness)
		if sel.HasMinMoon && moon < sel.MinMoon {
			return false
		}
		if sel.HasMaxMoon && moon > sel.MaxMoon {
			return false
		}
		return true
	case registry.SpawnConditionStructure:
		if ctx.InStructure == nil {
			return false
		}
		if sel.StructureTag != "" {
			members, ok := registry.StructureTagMembers(sel.StructureTag)
			if !ok {
				return false
			}
			return slices.ContainsFunc(members, ctx.InStructure)
		}
		return slices.ContainsFunc(sel.Structures, ctx.InStructure)
	default:
		return false
	}
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
