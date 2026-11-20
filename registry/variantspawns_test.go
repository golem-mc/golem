package registry

import (
	"slices"
	"testing"

	"github.com/golem-mc/golem/nbt"
)

// TestVariantSpawnsDecodePackEntries checks that ApplyPackRegistries decodes
// spawn_conditions for new and overridden variant entries while keeping
// built-in selectors for untouched entries.
func TestVariantSpawnsDecodePackEntries(t *testing.T) {
	t.Cleanup(func() { ApplyPackRegistries(nil) })

	ApplyPackRegistries(map[string]map[string]nbt.Tag{
		"minecraft:wolf_variant": {
			"test:frosty": nbt.Compound{
				"spawn_conditions": nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{
					nbt.Compound{
						"priority": nbt.Int(5),
						"condition": nbt.Compound{
							"type":   nbt.String("minecraft:biome"),
							"biomes": nbt.String("minecraft:plains"),
						},
					},
					nbt.Compound{
						"priority": nbt.Int(3),
						"condition": nbt.Compound{
							"type":  nbt.String("minecraft:moon_brightness"),
							"range": nbt.Compound{"min": nbt.Double(0.25), "max": nbt.Double(0.75)},
						},
					},
					nbt.Compound{
						"priority": nbt.Int(2),
						"condition": nbt.Compound{
							"type":       nbt.String("minecraft:structure"),
							"structures": nbt.List{Elem: nbt.TypeString, Items: []nbt.Tag{nbt.String("minecraft:igloo")}},
						},
					},
				}},
			},
		},
		"minecraft:pig_variant": {
			"minecraft:cold": nbt.Compound{
				"spawn_conditions": nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{
					nbt.Compound{
						"priority": nbt.Int(5),
						"condition": nbt.Compound{
							"type":   nbt.String("minecraft:biome"),
							"biomes": nbt.String("minecraft:plains"),
						},
					},
				}},
			},
		},
	})

	wolves := VariantSpawns("minecraft:wolf_variant")
	idx := slices.IndexFunc(wolves, func(e VariantSpawnEntry) bool { return e.Name == "test:frosty" })
	if idx < 0 {
		t.Fatal("test:frosty missing from VariantSpawns(wolf_variant)")
	}
	sels := wolves[idx].Selectors
	if len(sels) != 3 {
		t.Fatalf("test:frosty has %d selectors, want 3", len(sels))
	}
	if sels[0].Kind != SpawnConditionBiome || sels[0].Priority != 5 || !slices.Equal(sels[0].Biomes, []Biome{BiomePlains}) {
		t.Errorf("selector[0] = %+v", sels[0])
	}
	if sels[1].Kind != SpawnConditionMoonBrightness || sels[1].Priority != 3 || !sels[1].HasMinMoon || sels[1].MinMoon != 0.25 || !sels[1].HasMaxMoon || sels[1].MaxMoon != 0.75 {
		t.Errorf("selector[1] = %+v", sels[1])
	}
	if sels[2].Kind != SpawnConditionStructure || sels[2].Priority != 2 || !slices.Equal(sels[2].Structures, []string{"minecraft:igloo"}) {
		t.Errorf("selector[2] = %+v", sels[2])
	}

	pigs := VariantSpawns("minecraft:pig_variant")
	coldIdx := slices.IndexFunc(pigs, func(e VariantSpawnEntry) bool { return e.Name == "minecraft:cold" })
	if coldIdx < 0 || len(pigs[coldIdx].Selectors) != 1 || pigs[coldIdx].Selectors[0].Priority != 5 || !slices.Equal(pigs[coldIdx].Selectors[0].Biomes, []Biome{BiomePlains}) {
		t.Fatalf("overridden pig cold = %+v", pigs[coldIdx])
	}
	warmIdx := slices.IndexFunc(pigs, func(e VariantSpawnEntry) bool { return e.Name == "minecraft:warm" })
	if warmIdx < 0 || len(pigs[warmIdx].Selectors) != 1 || pigs[warmIdx].Selectors[0].BiomeTag != "minecraft:spawns_warm_variant_farm_animals" {
		t.Fatalf("untouched pig warm = %+v", pigs[warmIdx])
	}
}
