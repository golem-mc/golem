package combat

import (
	"fmt"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// vanilla: Entity.isInvulnerableToBase, LivingEntity.hurtServer, Enderman.hurtServer
func TestDamageCommandAppliesMobDamageTypeRules(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BowTestConfig(), "Tester", servertest.OpsListing(t, "Tester"), func(s *server.Server) {
		servertest.FreezeUntilHeld(t, s)
	})
	servertest.HoldTicks(t, srv)
	for _, tc := range []struct {
		kind    string
		typeID  int32
		damages []string
		want    float64
	}{
		{"iron_golem", protocol.EntityTypeIronGolem, []string{"fall", "ender_pearl", "stalagmite"}, 100},
		{"blaze", protocol.EntityTypeBlaze, []string{"freeze"}, 10},
		{"enderman", protocol.EntityTypeEnderman, []string{"arrow", "trident", "thrown", "mob_projectile", "fireball", "unattributed_fireball", "wind_charge", "wither_skull"}, 40},
	} {
		c.ChatCommand(fmt.Sprintf(`summon minecraft:%s 8.5 64 4.5 {NoAI:1b,Tags:["damage_rules"]}`, tc.kind))
		id := c.AwaitSpawn(tc.typeID).EntityID
		// Bind the world used by an enderman's immediate hurt teleports.
		servertest.RunTicks(t, srv, 1)
		for _, damage := range tc.damages {
			for _, cause := range []string{"", " by @s"} {
				c.ChatCommand("damage @e[tag=damage_rules,limit=1] 2 minecraft:" + damage + cause)
				c.Settle()
				if health, ok := srv.MobHealth(id); !ok || health != tc.want {
					t.Errorf("%s/%s%s: health %v (present %v), want %v", tc.kind, damage, cause, health, ok, tc.want)
				}
				servertest.RunTicks(t, srv, 11)
				// The blaze must start each hit with its full health.
				if tc.kind == "blaze" {
					srv.SetMobHealth(id, 20)
				}
			}
		}
		if tc.kind == "enderman" {
			x, y, z, _ := srv.MobPos(id)
			boat := srv.SpawnBoat(x, y, z, "minecraft:oak_boat")
			if !srv.MountMobOnBoat(id, boat) {
				t.Fatal("could not seat the enderman")
			}
			c.ChatCommand("damage @e[tag=damage_rules,limit=1] 2 minecraft:arrow")
			c.Settle()
			if health, ok := srv.MobHealth(id); !ok || health != 38 {
				t.Errorf("seated enderman: health %v (present %v), want 38", health, ok)
			}
			if srv.MobVehicleID(id) != boat {
				t.Error("the arrow damage dismounted the enderman")
			}
		}
		c.ChatCommand("tag @e[tag=damage_rules] remove damage_rules")
		c.Settle()
	}
}
