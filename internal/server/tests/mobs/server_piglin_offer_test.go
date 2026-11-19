package mobs

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// vanilla: PiglinAi.mobInteract, canAdmire, holdInOffhand
func TestAPiglinHoldingAShieldAcceptsAPlayersIngotAndDropsTheShield(t *testing.T) {
	t.Parallel()
	c, _, srv := barterWorld(t)
	p := srv.SpawnMob(barterPiglin, 3.5, 64, 0.5)
	c.ChatCommand("item replace entity @e[type=piglin,limit=1] weapon.offhand with minecraft:shield[minecraft:damage=7]")
	c.ChatCommand("give Slayer minecraft:gold_ingot 2")
	c.Settle()
	shield := srv.MobOffHand(p)
	if registry.Item(shield.ItemID) != registry.ItemShield {
		t.Fatalf("off hand = %v, want the shield", shield)
	}
	c.UseOn(p)
	c.Settle()
	servertest.RunTicks(t, srv, 1)
	if got := srv.MobOffHand(p); registry.Item(got.ItemID) != registry.ItemGoldIngot || got.Count != 1 {
		t.Fatalf("off hand = %v after the offer, want one gold ingot", got)
	}
	if got := srv.InventorySlot("Slayer", player.HotbarSlot); got.Count != 1 {
		t.Fatalf("player holds %v after the offer, want one remaining ingot", got)
	}
	items := srv.ItemsInDimension("minecraft:overworld")
	if len(items) != 1 || !items[0].Stack.Equal(shield) {
		t.Fatalf("dropped items = %+v, want the displaced shield with its damage", items)
	}
	if items[0].PickupDelay <= 0 {
		t.Error("the displaced shield has no pickup delay")
	}
	c.UseOn(p)
	c.Settle()
	if got := srv.InventorySlot("Slayer", player.HotbarSlot); got.Count != 1 {
		t.Fatalf("player holds %v after a second offer while admiring, want one ingot", got)
	}
	servertest.RunTicks(t, srv, 100)
	if got := srv.MobOffHand(p); registry.Item(got.ItemID) != registry.ItemGoldIngot {
		t.Fatal("the piglin let go of the ingot before finishing admiring")
	}
	servertest.RunTicks(t, srv, 25)
	if !srv.MobOffHand(p).Empty() {
		t.Fatal("the piglin still holds the ingot after admiring")
	}
	for _, it := range srv.ItemsInDimension("minecraft:overworld") {
		if it.Item != "minecraft:shield" && it.Item != "minecraft:gold_ingot" {
			return
		}
	}
	t.Fatal("the piglin threw no barter item after admiring")
}
