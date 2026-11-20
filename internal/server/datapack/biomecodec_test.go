package datapack

import (
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/golem-mc/golem/registry"
)

// TestDecodeBiomeVanillaPlains checks that vanilla's own plains.json decodes
// to the exact climate, spawner lists, spawn costs and creature probability.
// vanilla: data/minecraft/worldgen/biome/plains.json
func TestDecodeBiomeVanillaPlains(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile("testdata/biome/plains.json")
	if err != nil {
		t.Fatal(err)
	}
	got, err := DecodeBiome(data)
	if err != nil {
		t.Fatalf("DecodeBiome(plains.json): %v", err)
	}
	wantClimate := registry.BiomePlains.Climate()
	if got.Climate != wantClimate {
		t.Errorf("Climate = %+v, want %+v", got.Climate, wantClimate)
	}
	if got.CreatureProbability != 0.1 {
		t.Errorf("CreatureProbability = %v, want 0.1", got.CreatureProbability)
	}
	if len(got.SpawnCosts) != 0 {
		t.Errorf("SpawnCosts = %+v, want empty", got.SpawnCosts)
	}
	wantSpawners := [registry.MobCategoryCount][]BiomeSpawner{
		registry.MobCategoryMonster: {
			{registry.EntityTypeSpider, 100, 4, 4},
			{registry.EntityTypeZombie, 90, 4, 4},
			{registry.EntityTypeZombieVillager, 5, 1, 1},
			{registry.EntityTypeZombieHorse, 5, 1, 1},
			{registry.EntityTypeSkeleton, 100, 4, 4},
			{registry.EntityTypeCreeper, 100, 4, 4},
			{registry.EntityTypeSlime, 100, 4, 4},
			{registry.EntityTypeEnderman, 10, 1, 4},
			{registry.EntityTypeWitch, 5, 1, 1},
		},
		registry.MobCategoryCreature: {
			{registry.EntityTypeSheep, 12, 4, 4},
			{registry.EntityTypePig, 10, 4, 4},
			{registry.EntityTypeChicken, 10, 4, 4},
			{registry.EntityTypeCow, 8, 4, 4},
			{registry.EntityTypeHorse, 5, 2, 6},
			{registry.EntityTypeDonkey, 1, 1, 3},
		},
		registry.MobCategoryAmbient: {
			{registry.EntityTypeBat, 10, 8, 8},
		},
		registry.MobCategoryUndergroundWaterCreature: {
			{registry.EntityTypeGlowSquid, 10, 4, 6},
		},
	}
	for cat := range registry.MobCategoryCount {
		if !slices.Equal(got.Spawners[cat], wantSpawners[cat]) {
			t.Errorf("Spawners[%s] = %+v, want %+v", cat, got.Spawners[cat], wantSpawners[cat])
		}
	}
}

func TestDecodeBiomeAttributesAndModifiers(t *testing.T) {
	t.Parallel()
	raw := `{
		"has_precipitation": true,
		"temperature": -0.5,
		"temperature_modifier": "frozen",
		"downfall": 0.5,
		"attributes": {
			"minecraft:gameplay/creature_world_gen_spawn_probability": {"modifier": "multiply", "argument": 0.5},
			"minecraft:gameplay/natural_mob_spawns": {
				"spawns_by_category": {
					"creature": [
						{"type": "minecraft:cow", "weight": 0, "count": {"type": "minecraft:constant", "value": 3}},
						{"type": "minecraft:armor_stand", "weight": 5, "count": 2}
					],
					"monster": []
				},
				"spawn_costs": {
					"minecraft:skeleton": {"charge": 0.7, "energy_budget": 0.15},
					"minecraft:enderman": {"charge": 1.0, "energy_budget": 0.12}
				}
			}
		}
	}`
	got, err := DecodeBiome([]byte(raw))
	if err != nil {
		t.Fatalf("DecodeBiome: %v", err)
	}
	if want := (registry.BiomeClimate{Precipitation: true, Temperature: -0.5, Frozen: true}); got.Climate != want {
		t.Errorf("Climate = %+v, want %+v", got.Climate, want)
	}
	if got.CreatureProbability != 0.05 {
		t.Errorf("CreatureProbability = %v, want 0.05", got.CreatureProbability)
	}
	wantCreatures := []BiomeSpawner{
		{registry.EntityTypeCow, 0, 3, 3},
		{registry.EntityTypePig, 5, 2, 2}, // MISC coerced to PIG
	}
	if !slices.Equal(got.Spawners[registry.MobCategoryCreature], wantCreatures) {
		t.Errorf("creature spawners = %+v, want %+v", got.Spawners[registry.MobCategoryCreature], wantCreatures)
	}
	if len(got.Spawners[registry.MobCategoryMonster]) != 0 {
		t.Errorf("monster spawners = %+v, want empty", got.Spawners[registry.MobCategoryMonster])
	}
	wantCosts := []BiomeSpawnCost{
		{registry.EntityTypeEnderman, 1.0, 0.12},
		{registry.EntityTypeSkeleton, 0.7, 0.15},
	}
	if !slices.Equal(got.SpawnCosts, wantCosts) {
		t.Errorf("SpawnCosts = %+v, want %+v", got.SpawnCosts, wantCosts)
	}
}

func TestDecodeBiomeRejectsInvalid(t *testing.T) {
	t.Parallel()
	const base = `"has_precipitation":true,"temperature":0.8,"downfall":0.4`
	for _, tc := range []struct {
		name string
		json string
		want string
	}{
		{"missing has_precipitation", `{"temperature":0.8,"downfall":0.4}`, "has_precipitation"},
		{"missing temperature", `{"has_precipitation":true,"downfall":0.4}`, "temperature"},
		{"missing downfall", `{"has_precipitation":true,"temperature":0.8}`, "downfall"},
		{"unknown temperature_modifier", `{` + base + `,"temperature_modifier":"icy"}`, "temperature_modifier"},
		{"unknown entity in spawners", `{` + base + `,"attributes":{"minecraft:gameplay/natural_mob_spawns":{"spawns_by_category":{"monster":[{"type":"minecraft:nope","weight":10,"count":1}]},"spawn_costs":{}}}}`, "unknown entity type"},
		{"unknown entity in spawn_costs", `{` + base + `,"attributes":{"minecraft:gameplay/natural_mob_spawns":{"spawns_by_category":{},"spawn_costs":{"minecraft:nope":{"charge":1,"energy_budget":0.1}}}}}`, "unknown entity type"},
		{"negative weight", `{` + base + `,"attributes":{"minecraft:gameplay/natural_mob_spawns":{"spawns_by_category":{"monster":[{"type":"minecraft:zombie","weight":-1,"count":1}]},"spawn_costs":{}}}}`, "weight"},
		{"inverted uniform count", `{` + base + `,"attributes":{"minecraft:gameplay/natural_mob_spawns":{"spawns_by_category":{"monster":[{"type":"minecraft:zombie","weight":10,"count":{"type":"minecraft:uniform","min_inclusive":5,"max_inclusive":2}}]},"spawn_costs":{}}}}`, "count"},
		{"missing spawns_by_category", `{` + base + `,"attributes":{"minecraft:gameplay/natural_mob_spawns":{"spawn_costs":{}}}}`, "spawns_by_category"},
		{"missing spawn_costs", `{` + base + `,"attributes":{"minecraft:gameplay/natural_mob_spawns":{"spawns_by_category":{}}}}`, "spawn_costs"},
		{"override modifier", `{` + base + `,"attributes":{"minecraft:gameplay/natural_mob_spawns":{"modifier":"override","argument":{"spawns_by_category":{},"spawn_costs":{}}}}}`, "unknown modifier"},
		{"unknown modifier", `{` + base + `,"attributes":{"minecraft:gameplay/natural_mob_spawns":{"modifier":"append","argument":{"spawns_by_category":{},"spawn_costs":{}}}}}`, "unknown modifier"},
		{"unknown category", `{` + base + `,"attributes":{"minecraft:gameplay/natural_mob_spawns":{"spawns_by_category":{"bogus":[]},"spawn_costs":{}}}}`, "unknown mob category"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := DecodeBiome([]byte(tc.json))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("DecodeBiome() error = %v, want substring %q", err, tc.want)
			}
		})
	}
}
