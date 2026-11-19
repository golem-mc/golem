package mobs

import (
	"fmt"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// vanilla: SulfurCube.getSquishSound
func TestSulfurCubeLandingSound(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, body, sound string
		age, size         int
	}{
		{"filled adult", `equipment:{body:{id:"minecraft:stone",count:1}},`, "sulfur_cube.bounce", 0, 1},
		{"empty adult", "", "sulfur_cube.squish", 0, 1},
		{"empty tiny", "", "small_sulfur_cube.squish", -24000, 0},
		{"filled tiny", `equipment:{body:{id:"minecraft:stone",count:1}},`, "small_sulfur_cube.squish", -24000, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cfg := servertest.MobTestConfig()
			cfg.ViewDistance = config.MinDistance
			cfg.SimulationDistance = config.MinDistance
			srv, c := servertest.Join(t, cfg, "Keeper", servertest.OpsListing(t, "Keeper"))
			c.MoveToGround(4.5, 64, 0.5, true)
			c.Settle()
			servertest.HoldTicks(t, srv)
			c.ChatCommand(fmt.Sprintf(`summon minecraft:sulfur_cube 4.5 68 4.5 {%sAge:%d,Size:%d,PersistenceRequired:1b}`, tc.body, tc.age, tc.size))
			c.Settle()
			ids := srv.MobIDsOfKind("minecraft:sulfur_cube")
			if len(ids) != 1 {
				t.Fatalf("summoned cubes = %v, want one", ids)
			}
			if got := srv.MobEquipment(ids[0], "body"); got.Empty() != (tc.body == "") {
				t.Fatalf("cube body = %+v", got)
			}

			var landed string
			c.ReadTicks(srv, 30, func(id int32, data []byte) bool {
				if id != protocol.IDPlaySoundEffect {
					return false
				}
				name, _, _, _, _, _, _ := servertest.ReadAmbient(t, data)
				switch name {
				case "minecraft:entity.sulfur_cube.bounce", "minecraft:entity.sulfur_cube.squish", "minecraft:entity.small_sulfur_cube.squish":
					landed = name
					return true
				}
				return false
			})
			if want := "minecraft:entity." + tc.sound; landed != want {
				t.Errorf("landing sound = %q, want %q", landed, want)
			}
			if _, y, _, ok := srv.MobPos(ids[0]); !ok || y > 64.01 {
				t.Errorf("cube after landing: y=%g, present=%v", y, ok)
			}
		})
	}
}
