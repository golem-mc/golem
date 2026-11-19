package mobs

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"
)

// vanilla: LivingEntity.collectEquipmentChanges
func TestFrostWalkerBootsRefreezeAfterBareTicks(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"zombie", "armor_stand"} {
		t.Run(kind, func(t *testing.T) {
			srv, c, zombie := frostWalkerMobServer(t, "Reequip")
			selector, _ := srv.MobUUID(zombie)
			if kind == "armor_stand" {
				srv.SpawnArmorStand(5.5, 64, 5.5)
				selector = "@e[type=minecraft:armor_stand,limit=1]"
			}
			servertest.RunTicks(t, srv, 3)
			boots := `diamond_boots[enchantments={"minecraft:frost_walker":2}]`
			for cycle := range 2 {
				c.ChatCommand("item replace entity " + selector + " armor.feet with " + boots)
				c.Settle()
				servertest.RunTicks(t, srv, 1)
				servertest.ExpectBlockNamed(t, srv, 6, 63, 5, "minecraft:frosted_ice", nil)

				// Wear changes the stack without changing the body's feet cell.
				water := servertest.BlockState(t, "minecraft:water")
				srv.SetBlock(6, 63, 5, water)
				c.ChatCommand(`item replace entity ` + selector + ` armor.feet with diamond_boots[enchantments={"minecraft:frost_walker":2},damage=1]`)
				c.Settle()
				servertest.RunTicks(t, srv, 1)
				servertest.ExpectBlockNamed(t, srv, 6, 63, 5, "minecraft:frosted_ice", nil)

				c.ChatCommand("item replace entity " + selector + " armor.feet with air")
				c.Settle()
				srv.SetBlock(6, 63, 5, water)
				servertest.RunTicks(t, srv, 3)
				if got := srv.BlockAt(6, 63, 5); got != water {
					t.Fatalf("cycle %d: empty boots changed the water to %d", cycle, got)
				}
			}
		})
	}
}
