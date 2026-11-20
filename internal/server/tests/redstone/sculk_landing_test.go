package redstone

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// sculkLandingPhase is the phase of the sensor at (4, 64, 2).
func sculkLandingPhase(t *testing.T, srv *server.Server) string {
	t.Helper()
	_, props := servertest.StatePropsOf(t, srv, 4, 64, 2)
	return props["sculk_sensor_phase"]
}

// TestALooseBodyLandingActivatesASculkSensor: a body that falls six blocks
// onto the ground three and a half blocks from a sensor posts hit_ground
// (frequency 2); an item of wool dampens it.
// vanilla: Entity.checkFallDamage, ItemEntity.dampensVibrations
func TestALooseBodyLandingActivatesASculkSensor(t *testing.T) {
	t.Parallel()
	bodies := []struct {
		name  string
		spawn func(srv *server.Server) int32
		want  string
	}{
		{"item", func(srv *server.Server) int32 { return srv.SpawnItemEntity(0.5, 70, 2.5, "minecraft:stone", 1) }, "active"},
		{"wool", func(srv *server.Server) int32 { return srv.SpawnItemEntity(0.5, 70, 2.5, "minecraft:white_wool", 1) }, "inactive"},
		{"armorstand", func(srv *server.Server) int32 { return srv.SpawnArmorStand(0.5, 70, 2.5) }, "active"},
		{"tnt", func(srv *server.Server) int32 { return srv.SpawnTNT(0.5, 70, 2.5, 400) }, "active"},
		{"minecart", func(srv *server.Server) int32 { return srv.SpawnMinecart(0.5, 70, 2.5) }, "active"},
	}
	for _, b := range bodies {
		t.Run(b.name, func(t *testing.T) {
			t.Parallel()
			srv, c, _ := servertest.BoatPlayer(t, "creative", "Watcher", 0)
			srv.SetBlock(4, 64, 2, servertest.MustState(t, "minecraft:sculk_sensor", nil))
			if b.spawn(srv) == 0 {
				t.Fatal("spawn came to nothing")
			}
			c.Settle()
			if b.want == "active" {
				servertest.UntilTicks(t, srv, 60, "the sensor hears the landing", func() bool {
					return sculkLandingPhase(t, srv) == "active"
				})
				return
			}
			servertest.RunTicks(t, srv, 60)
			if phase := sculkLandingPhase(t, srv); phase != b.want {
				t.Fatalf("sensor phase = %q after the landing, want %s", phase, b.want)
			}
		})
	}
}

// TestAnArmorStandLandingMakesItsFallSound: a stand dropped six blocks says
// entity.armor_stand.fall, while a 3.5-block drop (below floor threshold), a
// landing on a hay bale (damageModifier 0.2), and a Silent stand stay quiet.
// vanilla: LivingEntity.causeFallDamage, LivingEntity.calculateFallDamage,
// ArmorStand.getFallSounds
func TestAnArmorStandLandingMakesItsFallSound(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name      string
		setup     func(t *testing.T, srv *server.Server)
		wantSound bool
	}{
		{
			name: "six_blocks",
			setup: func(t *testing.T, srv *server.Server) {
				if srv.SpawnArmorStand(0.5, 70, 2.5) == 0 {
					t.Fatal("spawn came to nothing")
				}
			},
			wantSound: true,
		},
		{
			name: "three_point_five_blocks",
			setup: func(t *testing.T, srv *server.Server) {
				if srv.SpawnArmorStand(0.5, 67.5, 2.5) == 0 {
					t.Fatal("spawn came to nothing")
				}
			},
		},
		{
			name: "hay_bale",
			setup: func(t *testing.T, srv *server.Server) {
				srv.SetBlock(0, 63, 2, servertest.MustState(t, "minecraft:hay_block", nil))
				if srv.SpawnArmorStand(0.5, 70, 2.5) == 0 {
					t.Fatal("spawn came to nothing")
				}
			},
		},
		{
			name: "silent",
			setup: func(t *testing.T, srv *server.Server) {
				srv.RunRconLine("summon minecraft:armor_stand 0.5 70 2.5 {Silent:1b}")
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv, c, _ := servertest.BoatPlayer(t, "creative", "Watcher", 0)
			servertest.HoldTicks(t, srv)
			tc.setup(t, srv)
			var heard []string
			c.ReadTicks(srv, 60, func(id int32, data []byte) bool {
				if id == protocol.IDPlaySoundEffect {
					name, _, _, _, _, _, _ := servertest.ReadAmbient(t, data)
					heard = append(heard, name)
				}
				return false
			})
			got := false
			for _, name := range heard {
				if name == "minecraft:entity.armor_stand.fall" {
					got = true
				}
			}
			if got != tc.wantSound {
				t.Fatalf("heard sounds %v; armor_stand.fall = %v, want %v", heard, got, tc.wantSound)
			}
		})
	}
}
