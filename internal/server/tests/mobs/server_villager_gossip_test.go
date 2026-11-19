package mobs

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

const gossipVillager = `minecraft:villager 1.5 64 0.5 {NoAI:1b,VillagerData:{profession:"minecraft:librarian",level:1,type:"minecraft:plains"},` +
	`Offers:{Recipes:[{buy:{id:"minecraft:emerald",count:10},sell:{id:"minecraft:paper",count:1},maxUses:16,priceMultiplier:0.2f}]}}`

func villagerType(t *testing.T) int32 {
	t.Helper()
	id, ok := registry.EntityTypeID("minecraft:villager")
	if !ok {
		t.Fatal("no villager entity type")
	}
	return id
}

// openedSpecialPrice opens the villager's trades and returns the first
// offer's special price; the window is closed again.
func openedSpecialPrice(t *testing.T, c *servertest.Client, id int32) int32 {
	t.Helper()
	c.UseOn(id)
	var payload []byte
	c.SettleAndCollect(func(pid int32, data []byte) {
		if pid == protocol.IDPlayMerchantOffers {
			payload = data
		}
	})
	if payload == nil {
		t.Fatal("no merchant offers")
	}
	r := protocol.NewReader(payload)
	window := r.ReadVarInt()
	r.ReadVarInt() // offers
	r.ReadVarInt() // cost item
	r.ReadVarInt() // cost count
	r.ReadVarInt() // predicate
	r.ReadSlot()
	r.ReadBool() // costB
	r.ReadBool() // out of stock
	r.ReadI32()  // uses
	r.ReadI32()  // max uses
	r.ReadI32()  // xp
	special := r.ReadI32()
	c.Send(protocol.IDPlayCloseContainerIn, func(b *protocol.Buffer) { b.PutVarInt(window) })
	c.Settle()
	return special
}

// TestHeroOfTheVillageDiscountsAVillagersTrades: the effect takes a third off
// a ten-emerald price (floored) while it lasts, and the discount is not kept.
// vanilla: Villager.updateSpecialPrices
func TestHeroOfTheVillageDiscountsAVillagersTrades(t *testing.T) {
	t.Parallel()
	const name = "Hero"
	srv, c, _ := servertest.JoinAs(t, servertest.MobTestConfig(), name, servertest.OpsListing(t, name))
	c.MoveToGround(0.5, 64, 0.5, true)
	c.Settle()
	servertest.HoldTicks(t, srv)
	c.ChatCommand("summon " + gossipVillager)
	id := c.AwaitSpawn(villagerType(t)).EntityID
	c.ChatCommand("effect give @s minecraft:hero_of_the_village 60 0")
	c.Settle()
	if got := openedSpecialPrice(t, c, id); got != -3 {
		t.Errorf("special price with the effect = %d, want -3", got)
	}
	c.ChatCommand("effect clear @s")
	c.Settle()
	if got := openedSpecialPrice(t, c, id); got != 0 {
		t.Errorf("special price without it = %d, want 0", got)
	}
}

// TestHurtingAVillagerRaisesItsPrices: a hit angers the villager (entity event
// 13) and costs the player reputation, which marks the prices up.
// vanilla: Villager.setLastHurtByMob, Villager.updateSpecialPrices
func TestHurtingAVillagerRaisesItsPrices(t *testing.T) {
	t.Parallel()
	const name = "Brute"
	srv, c, _ := servertest.JoinAs(t, servertest.MobTestConfig(), name, servertest.OpsListing(t, name))
	c.MoveToGround(0.5, 64, 0.5, true)
	c.Settle()
	servertest.HoldTicks(t, srv)
	c.ChatCommand("summon " + gossipVillager)
	id := c.AwaitSpawn(villagerType(t)).EntityID
	c.AttackLanded(srv, name, id)
	servertest.RunTicks(t, srv, 2)
	angry := false
	c.SettleAndCollect(func(pid int32, data []byte) {
		if pid != protocol.IDPlayEntityEvent {
			return
		}
		r := protocol.NewReader(data)
		if r.ReadI32() == id && r.ReadU8() == protocol.EntityStatusVillagerAngry {
			angry = true
		}
	})
	if !angry {
		t.Error("no angry villager event")
	}
	if got := openedSpecialPrice(t, c, id); got != 5 {
		t.Errorf("special price after a hit = %d, want 5", got)
	}
}
