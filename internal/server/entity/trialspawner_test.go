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

func TestTrialSpawnerAllowsPlacementDice(t *testing.T) {
	for _, kind := range []MobType{TypeGhast, TypeGuardian, TypeElderGuardian} {
		t.Run(kind.Name(), func(t *testing.T) {
			w := newSpawnTestWorld()
			w.sky = 15
			w.set(0, 63, 0, registry.BlockWater.DefaultState())
			rng, want := rand.New(rand.NewSource(1)), rand.New(rand.NewSource(1))
			for range 100 {
				expect := want.Intn(20) == 0
				if got := TrialSpawnerAllows(w, rng, kind, 0.5, 64, 0.5); got != expect {
					t.Fatalf("TrialSpawnerAllows = %v, want roll passing %v", got, expect)
				}
			}
			if rng.Int63() != want.Int63() {
				t.Fatal("placement consumed extra dice")
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

// TestTrialIdleReadsOnlyWhatMatters: the states that wait say so without a
// parse, and wake on the scan tick.
func TestTrialIdleReadsOnlyWhatMatters(t *testing.T) {
	t.Parallel()
	withMob := nbt.Compound{"spawn_data": nbt.Compound{"entity": nbt.Compound{"id": nbt.String("minecraft:zombie")}}}
	var scan int64
	for ; !trialScanTick(0, 64, 0, scan); scan++ {
	}
	if !TrialIdle(withMob, TrialWaitingForPlayers, false, 0, 64, 0, scan+1, true) {
		t.Error("waiting spawner between scans is not idle")
	}
	if TrialIdle(withMob, TrialWaitingForPlayers, false, 0, 64, 0, scan, true) {
		t.Error("waiting spawner on its scan tick is idle")
	}
	if TrialIdle(withMob, TrialWaitingForPlayers, false, 0, 64, 0, scan+1, false) {
		t.Error("waiting spawner that cannot spawn is idle")
	}
	if TrialIdle(nbt.Compound{}, TrialInactive, false, 0, 64, 0, 5, true) {
		t.Error("inactive spawner without spawn_data is idle: it has to store the empty one")
	}
	if !TrialIdle(nbt.Compound{"spawn_data": nbt.Compound{"entity": nbt.Compound{}}}, TrialInactive, false, 0, 64, 0, 5, true) {
		t.Error("inactive spawner with an empty spawn_data is not idle")
	}
	if TrialIdle(withMob, TrialInactive, false, 0, 64, 0, 5, true) {
		t.Error("inactive spawner with a mob is idle")
	}
	cooling := nbt.Compound{"cooldown_ends_at": nbt.Long(scan + 50)}
	if !TrialIdle(cooling, TrialCooldown, false, 0, 64, 0, scan+1, true) {
		t.Error("cooling spawner before its end is not idle")
	}
	if TrialIdle(cooling, TrialCooldown, false, 0, 64, 0, scan+50, true) {
		t.Error("cooling spawner at its end is idle")
	}
}

// TestOverrideTrialEntityKeepsTheEjectingLootTable: the reset leaves the loot
// table a finished spawner ejects.
// vanilla: TrialSpawnerStateData.reset
func TestOverrideTrialEntityKeepsTheEjectingLootTable(t *testing.T) {
	t.Parallel()
	stored := nbt.Compound{"ejecting_loot_table": nbt.String("minecraft:spawners/trial_chamber/consumables")}
	out := OverrideTrialEntity(stored, TypeZombie, nil)
	if got, _ := nbt.Get[nbt.String](out, "ejecting_loot_table"); got == "" {
		t.Error("the override dropped ejecting_loot_table")
	}
}

// TestTrialSpawnerConfigByRegistryName: a config saved as a registry name
// reads as the data pack's definition, and every shipped config parses.
// vanilla: TrialSpawner.Config
func TestTrialSpawnerConfigByRegistryName(t *testing.T) {
	t.Parallel()
	tag := nbt.Compound{
		"normal_config":  nbt.String("minecraft:trial_chamber/small_melee/slime/normal"),
		"ominous_config": nbt.String("trial_chamber/breeze/ominous"),
	}
	sp := ParseTrialSpawner(tag)
	if len(sp.normal.potentials) != 2 || sp.normal.potentials[0].weight != 3 {
		t.Errorf("slime potentials = %+v", sp.normal.potentials)
	}
	if sp.normal.simultaneousMobs != 3 || sp.normal.ticksBetweenSpawn != 20 {
		t.Errorf("slime config = %+v", sp.normal)
	}
	if sp.ominousCfg.totalMobs != 4 || len(sp.ominousCfg.lootTables) != 2 {
		t.Errorf("breeze ominous config = %+v", sp.ominousCfg)
	}
	for name := range trialSpawnerConfigs {
		cfg := trialConfigTag(nbt.Compound{"c": nbt.String(name)}, "c")
		if list, _ := nbt.Get[nbt.List](cfg, "spawn_potentials"); len(list.Items) == 0 {
			t.Errorf("%s does not parse", name)
		}
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
