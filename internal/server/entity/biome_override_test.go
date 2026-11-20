package entity

import (
	"os"
	"slices"
	"testing"

	"github.com/golem-mc/golem/internal/server/datapack"
	"github.com/golem-mc/golem/registry"
)

// TestDecodeVanillaPlainsMatchesGeneratedTables checks that decoding vanilla's
// own plains.json produces the exact entries generated into spawning_gen.go.
func TestDecodeVanillaPlainsMatchesGeneratedTables(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile("../datapack/testdata/biome/plains.json")
	if err != nil {
		t.Fatal(err)
	}
	ov, err := datapack.DecodeBiome(data)
	if err != nil {
		t.Fatalf("DecodeBiome(plains.json): %v", err)
	}
	for cat := range SpawnCategory(SpawnCategoryCount) {
		var got []spawnData
		for _, e := range ov.Spawners[cat] {
			got = append(got, spawnData{e.Type, e.Weight, e.MinCount, e.MaxCount})
		}
		var want []spawnData
		if lists := spawnListsOf(cat); lists != nil {
			want = lists[registry.BiomePlains]
		}
		if !slices.Equal(got, want) {
			t.Errorf("category %s = %+v, want %+v", cat, got, want)
		}
	}
	var gotCosts []spawnCost
	for _, c := range ov.SpawnCosts {
		gotCosts = append(gotCosts, spawnCost{c.Type, c.Charge, c.Budget})
	}
	if want := biomeSpawnCosts[registry.BiomePlains]; !slices.Equal(gotCosts, want) {
		t.Errorf("spawn_costs = %+v, want %+v", gotCosts, want)
	}
	if ov.CreatureProbability != creatureSpawnProbability {
		t.Errorf("creature probability = %v, want %v", ov.CreatureProbability, creatureSpawnProbability)
	}
}

// TestApplyPackBiomeSpawnsOverridesListsCostsAndProbability verifies that
// ApplyPackBiomeSpawns overrides spawn lists, spawn costs, HasSpawnCost and
// chunk-generation creature probability (including 0.0), and restores defaults
// when cleared.
func TestApplyPackBiomeSpawnsOverridesListsCostsAndProbability(t *testing.T) {
	t.Cleanup(func() { ApplyPackBiomeSpawns(nil) })

	ov := datapack.BiomeOverride{
		SpawnCosts: []datapack.BiomeSpawnCost{
			{Type: registry.EntityTypeCow, Charge: 0.8, Budget: 0.2},
		},
		CreatureProbability: 0,
	}
	ov.Spawners[CategoryCreature] = []datapack.BiomeSpawner{
		{Type: registry.EntityTypeCow, Weight: 10, MinCount: 2, MaxCount: 2},
	}
	ApplyPackBiomeSpawns(map[registry.Biome]datapack.BiomeOverride{
		registry.BiomePlains: ov,
	})

	if got := spawnListFor(CategoryMonster, registry.BiomePlains); len(got) != 0 {
		t.Errorf("plains monster list = %+v, want empty", got)
	}
	if got, want := spawnListFor(CategoryCreature, registry.BiomePlains), []spawnData{{registry.EntityTypeCow, 10, 2, 2}}; !slices.Equal(got, want) {
		t.Errorf("plains creature list = %+v, want %+v", got, want)
	}
	if !HasSpawnCost(registry.EntityTypeCow) {
		t.Error("HasSpawnCost(cow) = false, want true")
	}
	if charge, ok := SpawnChargeOf(registry.BiomePlains, registry.EntityTypeCow); !ok || charge != 0.8 {
		t.Errorf("SpawnChargeOf(plains, cow) = (%v, %v), want (0.8, true)", charge, ok)
	}
	// Explicit 0.0 creature probability must suppress chunk-generation spawns,
	// not fall back to the 0.1 default.
	w := newSpawnTestWorld()
	var spawned int
	SpawnOriginalMobs(w, &scriptedDice{floats: []float32{0.0}}, &scriptedDice{}, ChunkPos{}, func(SpawnRequest) {
		spawned++
	})
	if spawned != 0 {
		t.Errorf("SpawnOriginalMobs with probability 0 spawned %d mobs, want 0", spawned)
	}

	// Clearing restores the built-in tables.
	ApplyPackBiomeSpawns(nil)
	if len(spawnListFor(CategoryMonster, registry.BiomePlains)) == 0 {
		t.Error("plains monster list still empty after clear")
	}
	if HasSpawnCost(registry.EntityTypeCow) {
		t.Error("HasSpawnCost(cow) still true after clear")
	}
}
