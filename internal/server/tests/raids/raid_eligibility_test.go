package raids

import (
	"fmt"
	"testing"

	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// vanilla: Raider.finalizeSpawn
func TestOnlyNonNaturalWitchesJoinRaids(t *testing.T) {
	t.Parallel()
	srv, c, bell := village(t, rules.DifficultyNormal)
	startRaid(t, c, srv, 0)
	x, y, z := float64(bell[0])+2.5, float64(bell[1]), float64(bell[2])+0.5
	egg := srv.SpawnMob("minecraft:witch", x, y, z)
	natural := srv.SpawnMobNatural("minecraft:witch", x+2, y, z)
	srv.SetMobNoAI(egg)
	srv.SetMobNoAI(natural)
	servertest.RunTicks(t, srv, 40)
	if in, _ := srv.MobRaid(egg); !in {
		t.Error("the spawn-egg witch did not join the raid")
	}
	if in, _ := srv.MobRaid(natural); in {
		t.Error("the natural witch joined the raid")
	}
}

// vanilla: SummonCommand.createEntity, Raider.readAdditionalSaveData
func TestTaggedRaidersKeepTheirSavedEligibility(t *testing.T) {
	t.Parallel()
	srv, c, _ := village(t, rules.DifficultyNormal)
	startRaid(t, c, srv, 0)
	for _, tc := range []struct {
		kind, tag string
		want      bool
	}{
		{"witch", "", true},
		{"witch", "{}", false},
		{"pillager", "{}", false},
		{"witch", "{CanJoinRaid:1b}", true},
		{"pillager", "{CanJoinRaid:0b}", false},
	} {
		t.Run(tc.kind+tc.tag, func(t *testing.T) {
			before := make(map[int32]bool)
			for _, m := range srv.Mobs() {
				before[m.ID] = true
			}
			line := fmt.Sprintf("summon minecraft:%s 6.5 64 6.5", tc.kind)
			if tc.tag != "" {
				line += " " + tc.tag
			}
			srv.RunLine(line)
			var id int32
			for _, m := range srv.Mobs() {
				if !before[m.ID] {
					id = m.ID
				}
			}
			if id == 0 {
				t.Fatal("summon produced no mob")
			}
			srv.SetMobNoAI(id)
			servertest.RunTicks(t, srv, 40)
			if in, _ := srv.MobRaid(id); in != tc.want {
				t.Errorf("joined raid = %v, want %v", in, tc.want)
			}
		})
	}
}

// vanilla: BaseSpawner.serverTick, TrialSpawner.spawnMob
func TestSpawnerWitchesJoinRaids(t *testing.T) {
	for _, trial := range []bool{false, true} {
		t.Run(fmt.Sprintf("trial=%v", trial), func(t *testing.T) {
			t.Parallel()
			srv, c, _ := village(t, rules.DifficultyNormal)
			startRaid(t, c, srv, 0)
			srv.SetDayTime(18000)
			data := nbt.Compound{
				"entity": nbt.Compound{"id": nbt.String("minecraft:witch")},
				"custom_spawn_rules": nbt.Compound{
					"block_light_limit": nbt.List{Elem: nbt.TypeInt, Items: []nbt.Tag{nbt.Int(0), nbt.Int(15)}},
					"sky_light_limit":   nbt.List{Elem: nbt.TypeInt, Items: []nbt.Tag{nbt.Int(0), nbt.Int(15)}},
				},
			}
			if trial {
				srv.SetBlock(4, 64, 4, registry.MustState("minecraft:trial_spawner", nil))
				srv.SetBlockEntityTagAt(4, 64, 4, "minecraft:trial_spawner", nbt.Compound{
					"normal_config": nbt.Compound{
						"total_mobs":                         nbt.Float(1),
						"simultaneous_mobs":                  nbt.Float(1),
						"total_mobs_added_per_player":        nbt.Float(0),
						"simultaneous_mobs_added_per_player": nbt.Float(0),
						"spawn_potentials": nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{nbt.Compound{
							"weight": nbt.Int(1), "data": data,
						}}},
					},
				})
			} else {
				srv.SetBlock(4, 64, 4, registry.MustState("minecraft:spawner", nil))
				srv.SetBlockEntityTagAt(4, 64, 4, "minecraft:mob_spawner", nbt.Compound{
					"Delay": nbt.Short(0), "SpawnCount": nbt.Short(1), "SpawnData": data,
				})
			}
			var id int32
			servertest.UntilTicks(t, srv, 200, "the spawner witch arrives", func() bool {
				for _, m := range srv.Mobs() {
					if m.Kind == "minecraft:witch" {
						id = m.ID
						return true
					}
				}
				return false
			})
			srv.SetMobNoAI(id)
			servertest.RunTicks(t, srv, 40)
			if in, _ := srv.MobRaid(id); !in {
				t.Error("the spawner witch did not join the raid")
			}
		})
	}
}
