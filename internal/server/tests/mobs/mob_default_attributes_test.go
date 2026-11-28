package mobs

import (
	"strconv"
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"
)

// TestMobDefaultAttributes: the base attributes of the kinds whose defaults
// came from the wrong supplier.
// vanilla: Mob.createMobAttributes, Monster.createMonsterAttributes,
// Hoglin.createAttributes, EnderDragon.createAttributes, Giant.createAttributes
func TestMobDefaultAttributes(t *testing.T) {
	t.Parallel()
	cfg := servertest.MobTestConfig()
	cfg.ViewDistance, cfg.SimulationDistance = 2, 2
	srv, _ := servertest.Join(t, cfg, "Keeper", servertest.OpsListing(t, "Keeper"))
	servertest.HoldTicks(t, srv)
	cases := []struct {
		mob, attr string
		want      float64
		none      bool // the kind has no such attribute
	}{
		{"ender_dragon", "camera_distance", 16, false},
		{"giant", "camera_distance", 16, false},
		{"ender_dragon", "movement_speed", 0.7, false},
		{"shulker", "movement_speed", 0.7, false},
		{"vex", "movement_speed", 0.7, false},
		{"hoglin", "attack_knockback", 1, false},
		{"zoglin", "attack_knockback", 1, false},
		{"evoker", "attack_damage", 2, false},
		{"illusioner", "attack_damage", 2, false},
		{"wither", "attack_damage", 2, false},
		{"ghast", "attack_damage", 0, true},
		{"shulker", "attack_damage", 0, true},
		{"wither_skeleton", "attack_damage", 2, false},
	}
	for _, tc := range cases {
		srv.RunLine("kill @e[type=!player]")
		srv.RunLine("summon minecraft:" + tc.mob + " 0.5 100 0.5 {NoAI:1b,PersistenceRequired:1b,Silent:1b}")
		got := srv.RunLine("attribute @e[type=" + tc.mob + ",limit=1] minecraft:" + tc.attr + " get")
		if len(got) != 1 {
			t.Errorf("%s %s: %+v", tc.mob, tc.attr, got)
			continue
		}
		if tc.none {
			if got[0].Translate == "commands.attribute.base_value.get.success" {
				t.Errorf("%s has %s: %+v", tc.mob, tc.attr, got)
			}
			continue
		}
		v, err := strconv.ParseFloat(got[0].With[len(got[0].With)-1].Plain(), 64)
		if err != nil || v != tc.want {
			t.Errorf("%s %s = %v, want %v (%+v)", tc.mob, tc.attr, v, tc.want, got)
		}
	}
}

// TestWitherSkeletonSpawnedPlainAttackDamage: finalizeSpawn raises the base to
// 4 for a summon without NBT (the stone sword adds its modifier on top).
// vanilla: WitherSkeleton.finalizeSpawn
func TestWitherSkeletonSpawnedPlainAttackDamage(t *testing.T) {
	t.Parallel()
	cfg := servertest.MobTestConfig()
	cfg.ViewDistance, cfg.SimulationDistance = 2, 2
	srv, _ := servertest.Join(t, cfg, "Keeper", servertest.OpsListing(t, "Keeper"))
	servertest.HoldTicks(t, srv)
	srv.RunLine("summon minecraft:wither_skeleton 0.5 100 0.5")
	got := srv.RunLine("attribute @e[type=wither_skeleton,limit=1] minecraft:attack_damage base get")
	if len(got) != 1 || got[0].With[len(got[0].With)-1].Plain() != "4.0" {
		t.Errorf("attack_damage: %+v", got)
	}
}
