package mobs

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// TestAVillagerThrowsAGiftToAHeroOfTheVillage: after the first wait, a
// villager in view of a player with the effect throws an item at them.
// vanilla: GiveGiftToHero
func TestAVillagerThrowsAGiftToAHeroOfTheVillage(t *testing.T) {
	t.Parallel()
	const name = "Hero"
	srv, c, _ := servertest.JoinAs(t, servertest.MobTestConfig(), name, servertest.OpsListing(t, name))
	c.MoveToGround(0.5, 64, 0.5, true)
	c.Settle()
	servertest.HoldTicks(t, srv)
	// A fence keeps the villager in view without hiding it from the hero.
	c.ChatCommand("fill 2 64 -1 4 64 1 minecraft:oak_fence hollow")
	c.ChatCommand("summon minecraft:villager 3.5 64 0.5 {VillagerData:{profession:\"minecraft:farmer\",level:1,type:\"minecraft:plains\"}}")
	c.AwaitSpawn(villagerType(t))
	c.ChatCommand("effect give @s minecraft:hero_of_the_village 1000 0")
	c.Settle()
	item, ok := registry.EntityTypeID("minecraft:item")
	if !ok {
		t.Fatal("no item entity type")
	}
	// The loop is run in steps so the client answers keep-alives.
	for range 14 {
		servertest.RunTicks(t, srv, 100)
		thrown := false
		c.SettleAndCollect(func(pid int32, data []byte) {
			if pid != protocol.IDPlaySpawnEntity {
				return
			}
			r := protocol.NewReader(data)
			r.ReadVarInt() // entity ID
			r.ReadUUID()
			if r.ReadVarInt() == item {
				thrown = true
			}
		})
		if thrown {
			return
		}
	}
	t.Fatal("the villager threw no gift")
}
