package entity

import (
	"math/rand"
	"testing"

	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// vanilla: SpawnPlacements.checkSpawnRules, EntitySpawnReason.ignoresLightRequirements
func TestTrialSpawnerAllowsPlacementPredicates(t *testing.T) {
	for _, tc := range []struct {
		name   string
		kind   MobType
		light  uint8
		ground registry.Block
		want   bool
	}{
		{"pillager bright", TypePillager, 9, registry.BlockStone, false},
		{"pillager dim", TypePillager, 8, registry.BlockStone, true},
		{"zombie full light", TypeZombie, 15, registry.BlockStone, true},
		{"drowned dry full light", TypeDrowned, 15, registry.BlockStone, true},
		{"piglin wart", TypePiglin, 15, registry.BlockNetherWartBlock, false},
		{"hoglin wart", TypeHoglin, 15, registry.BlockNetherWartBlock, false},
		{"zombified piglin wart", TypeZombifiedPiglin, 15, registry.BlockNetherWartBlock, false},
		{"piglin stone", TypePiglin, 15, registry.BlockStone, true},
		{"hoglin stone", TypeHoglin, 15, registry.BlockStone, true},
		{"guardian dry", TypeGuardian, 15, registry.BlockStone, false},
		{"guardian water below", TypeGuardian, 15, registry.BlockWater, true},
		{"elder guardian dry", TypeElderGuardian, 15, registry.BlockStone, false},
		{"elder guardian water below", TypeElderGuardian, 15, registry.BlockWater, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := newSpawnTestWorld()
			w.sky = 0
			w.set(0, 63, 0, tc.ground.DefaultState())
			w.blockLights[[3]int32{0, 64, 0}] = tc.light
			if got := TrialSpawnerAllows(w, rand.New(rand.NewSource(1)), tc.kind, 0.5, 64, 0.5); got != tc.want {
				t.Errorf("TrialSpawnerAllows = %v, want %v", got, tc.want)
			}
		})
	}
}

func BenchmarkTrialSpawnerAllows(b *testing.B) {
	w := newSpawnTestWorld()
	w.sky = 0
	w.set(0, 63, 0, registry.BlockWater.DefaultState())
	w.blockLights[[3]int32{0, 64, 0}] = 15
	for _, kind := range []MobType{TypeZombie, TypePillager, TypeGhast, TypeGuardian, TypePiglin, TypeHoglin} {
		b.Run(kind.Name(), func(b *testing.B) {
			rng := rand.New(rand.NewSource(1))
			b.ReportAllocs()
			for b.Loop() {
				TrialSpawnerAllows(w, rng, kind, 0.5, 64, 0.5)
			}
		})
	}
}

// TestOverrideTrialEntityResetsAndConfigures: a spawn egg's override clears
// the saved data and makes both configs spawn the one mob, keeping the other
// config fields.
// vanilla: TrialSpawner.overrideEntityToSpawn
func TestOverrideTrialEntityResetsAndConfigures(t *testing.T) {
	t.Parallel()
	stored := nbt.Compound{
		"total_mobs_spawned": nbt.Int(4),
		"normal_config":      nbt.Compound{"total_mobs": nbt.Float(9)},
		"spawn_data":         nbt.Compound{"entity": nbt.Compound{"id": nbt.String("minecraft:husk")}},
	}
	out := OverrideTrialEntity(stored, TypeZombie, nil)
	if _, has := out["spawn_data"]; has {
		t.Error("spawn_data survived the override")
	}
	if n, _ := nbt.Get[nbt.Int](out, "total_mobs_spawned"); n != 0 {
		t.Errorf("total_mobs_spawned = %d, want 0", n)
	}
	for _, key := range []string{"normal_config", "ominous_config"} {
		cfg, _ := nbt.Get[nbt.Compound](out, key)
		list, _ := nbt.Get[nbt.List](cfg, "spawn_potentials")
		if len(list.Items) != 1 {
			t.Fatalf("%s has %d potentials, want 1", key, len(list.Items))
		}
		data, _ := nbt.Get[nbt.Compound](list.Items[0].(nbt.Compound), "data")
		entity, _ := nbt.Get[nbt.Compound](data, "entity")
		if id, _ := nbt.Get[nbt.String](entity, "id"); id != nbt.String(TypeZombie.Name()) {
			t.Errorf("%s spawns %q, want the zombie", key, id)
		}
	}
	if total, _ := nbt.Get[nbt.Float](out["normal_config"].(nbt.Compound), "total_mobs"); total != 9 {
		t.Errorf("total_mobs = %v, want the config's 9 kept", total)
	}
	if _, has := stored["normal_config"].(nbt.Compound)["spawn_potentials"]; has {
		t.Error("the override changed the stored compound")
	}
}

func BenchmarkParseTrialSpawner(b *testing.B) {
	named := nbt.Compound{
		"normal_config":  nbt.String("minecraft:trial_chamber/melee/zombie/normal"),
		"ominous_config": nbt.String("minecraft:trial_chamber/melee/zombie/ominous"),
	}
	inline := nbt.Compound{
		"normal_config":  trialConfigTag(named, "normal_config"),
		"ominous_config": trialConfigTag(named, "ominous_config"),
	}
	for name, tag := range map[string]nbt.Compound{"named": named, "inline": inline} {
		b.Run(name, func(b *testing.B) {
			for b.Loop() {
				ParseTrialSpawner(tag)
			}
		})
	}
}
