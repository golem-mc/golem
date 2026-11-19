package mobs

import (
	"strconv"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
)

func TestAllaySavesEffectiveLootPickup(t *testing.T) {
	t.Parallel()
	srv, c := servertest.BreedingPlayer(t, "survival", "Keeper", "minecraft:diamond", 1, servertest.OpsListing(t, "Keeper"))
	servertest.HoldTicks(t, srv)
	c.ChatCommand(`summon minecraft:allay 2.5 65 0.5 {CanPickUpLoot:0b}`)
	id := c.ExpectMobSpawn(protocol.EntityTypeAllay).EntityID
	selector := "@e[type=minecraft:allay,limit=1] "
	expect := func(want string) {
		t.Helper()
		if got := entityData(t, c, selector+"CanPickUpLoot"); got != want {
			t.Fatalf("CanPickUpLoot = %s, want %s", got, want)
		}
	}
	expect("0b")
	c.UseOn(id)
	c.Settle()
	expect("1b")
	c.UseOn(id)
	c.Settle()
	expect("0b")
	c.UseOn(id)
	c.Settle()
	c.ChatCommand(`data merge entity @e[type=minecraft:allay,limit=1] {Inventory:[{Slot:0b,id:"minecraft:diamond",count:1}]}`)
	c.Settle()
	if len(srv.MobInventory(id)) != 1 {
		t.Fatal("the allay has no diamond to deliver")
	}
	servertest.UntilTicks(t, srv, 100, "the allay throwing its diamond", func() bool {
		return len(srv.MobInventory(id)) == 0
	})
	expect("0b")
	wait, err := strconv.Atoi(entityData(t, c, selector+`Brain.memories."minecraft:item_pickup_cooldown_ticks".value`))
	if err != nil || wait < 1 || wait > 60 {
		t.Fatalf("pickup cooldown = %d, err %v; want 1..60 ticks", wait, err)
	}
	servertest.RunTicks(t, srv, wait-1)
	if got := entityData(t, c, selector+`Brain.memories."minecraft:item_pickup_cooldown_ticks".value`); got != "1" {
		t.Fatalf("pickup cooldown = %s, want 1", got)
	}
	expect("0b")
	// vanilla: CountDownCooldownTicks.stop erases the zero-valued memory next tick.
	servertest.RunTicks(t, srv, 1)
	if got := entityData(t, c, selector+`Brain.memories."minecraft:item_pickup_cooldown_ticks".value`); got != "0" {
		t.Fatalf("pickup cooldown = %s, want 0", got)
	}
	expect("0b")
	servertest.RunTicks(t, srv, 1)
	if memories := entityCompound(t, c, selector+"Brain.memories"); memories["minecraft:item_pickup_cooldown_ticks"] != nil {
		t.Fatalf("pickup cooldown memory remains: %v", memories)
	}
	expect("1b")
}
