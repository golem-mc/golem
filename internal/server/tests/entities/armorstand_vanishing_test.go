package entities

import (
	"fmt"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

func TestArmorStandBreakDestroysVanishingEquipment(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.MobTestConfig(), "Attacker", servertest.OpsListing(t, "Attacker"))
	servertest.HoldTicks(t, srv)
	servertest.RunTicks(t, srv, 10)

	id := srv.SpawnArmorStand(3.5, 64, 3.5)
	c.ExpectArmorStandSpawn()
	stand := armorStandAt(t, srv, id)
	c.ChatCommand(fmt.Sprintf(`item replace entity %s armor.head with diamond_helmet[enchantments={"minecraft:vanishing_curse":1}]`, stand.UUID))
	helmet := servertest.EquipEntry(c.ExpectEquipment(id), 5)
	if registry.Item(helmet.ItemID) != registry.ItemDiamondHelmet || rules.EnchantmentLevel(helmet, "minecraft:vanishing_curse") != 1 {
		t.Fatalf("head equipment = %+v, want a diamond helmet with Curse of Vanishing I", helmet)
	}
	c.ChatCommand(fmt.Sprintf("item replace entity %s armor.feet with iron_boots", stand.UUID))
	c.MoveToGrounded(3.5, 64, 5.5, 180, 40, true)
	c.Settle()

	c.Attack(id)
	c.ExpectEntityEvent(id, protocol.EntityStatusArmorStandShake)
	c.Attack(id)
	c.ExpectRemoval(id)
	c.Settle()

	servertest.UntilTicks(t, srv, 5, "the stand and its boots drop", func() bool {
		drops := map[registry.Item]int32{}
		for _, item := range srv.ItemsInDimension(world.DimensionName) {
			drops[registry.Item(item.Stack.ItemID)] += item.Count
		}
		return drops[registry.ItemArmorStand] == 1 && drops[registry.ItemIronBoots] == 1
	})
	for _, item := range srv.ItemsInDimension(world.DimensionName) {
		if registry.Item(item.Stack.ItemID) == registry.ItemDiamondHelmet {
			t.Error("the broken stand dropped its Curse of Vanishing helmet")
		}
	}
}
