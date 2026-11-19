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

func TestAllayLoadedPickupCooldownKeepsZeroMemory(t *testing.T) {
	t.Parallel()
	cfg := servertest.MobTestConfig()
	cfg.GameMode = "creative"
	srv, c := servertest.Join(t, cfg, "Observer", servertest.OpsListing(t, "Observer"))
	servertest.HoldTicks(t, srv)
	c.ChatCommand(`summon minecraft:allay 4.5 65 0.5 {equipment:{mainhand:{id:"minecraft:diamond",count:1}},Brain:{memories:{"minecraft:item_pickup_cooldown_ticks":{value:1}}}}`)
	id := c.ExpectMobSpawn(protocol.EntityTypeAllay).EntityID
	selector := "@e[type=minecraft:allay,limit=1] "
	srv.SpawnItemEntityDelay(4.5, 65, 0.5, "minecraft:diamond", 1, 0)
	servertest.RunTicks(t, srv, 1)
	if got := entityData(t, c, selector+`Brain.memories."minecraft:item_pickup_cooldown_ticks".value`); got != "0" {
		t.Fatalf("pickup cooldown = %s, want 0", got)
	}
	// A /data merge saves and reloads the brain, including the zero-valued memory.
	c.ChatCommand(`data merge entity @e[type=minecraft:allay,limit=1] {PersistenceRequired:1b}`)
	c.Settle()
	if got := entityData(t, c, selector+"CanPickUpLoot"); got != "0b" {
		t.Fatalf("pickup at zero = %s, want 0b", got)
	}
	if inv := srv.MobInventory(id); len(inv) != 0 {
		t.Fatalf("picked up during cooldown: %v", inv)
	}
	servertest.RunTicks(t, srv, 1)
	if got := entityData(t, c, selector+"CanPickUpLoot"); got != "1b" {
		t.Fatalf("pickup after removal = %s, want 1b", got)
	}
	servertest.UntilTicks(t, srv, 10, "the allay picking up after memory removal", func() bool {
		return len(srv.MobInventory(id)) == 1
	})
}

func TestSulfurCubeSavesEffectiveLootPickup(t *testing.T) {
	t.Parallel()
	cfg := servertest.MobTestConfig()
	cfg.GameMode = "creative"
	srv, c := servertest.Join(t, cfg, "Observer", servertest.OpsListing(t, "Observer"))
	servertest.HoldTicks(t, srv)
	c.ChatCommand(`summon minecraft:sulfur_cube 3.5 64 0.5 {NoAI:1b,CanPickUpLoot:0b}`)
	c.ExpectMobSpawn(protocol.EntityTypeSulfurCube)
	selector := "@e[type=minecraft:sulfur_cube,limit=1] "
	for _, tc := range []struct{ item, want string }{
		{"", "1b"}, {"minecraft:stone", "0b"}, {"minecraft:air", "1b"},
	} {
		if tc.item != "" {
			c.ChatCommand("data merge entity " + selector + `{equipment:{body:{id:"` + tc.item + `",count:1}}}`)
			c.Settle()
		}
		if got := entityData(t, c, selector+"CanPickUpLoot"); got != tc.want {
			t.Fatalf("body set to %q: CanPickUpLoot = %s, want %s", tc.item, got, tc.want)
		}
	}
}
