package mobs

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

func TestAWeakZombieVillagerEatsAnAppleAndCuresWithItsTrades(t *testing.T) {
	t.Parallel()
	const name = "Healer"
	srv, c, _ := servertest.JoinAs(t, servertest.HostileTestConfig(), name, servertest.OpsListing(t, name))
	servertest.HoldTicks(t, srv)
	srv.SetDayTime(18000)
	srv.SetPlayerGameMode(name, protocol.GameModeSurvival)
	c.MoveToGround(0.5, 64, 0.5, true)
	srv.GiveSlot(name, player.HotbarSlot, protocol.ItemStack{ItemID: registry.MustItem("minecraft:golden_apple"), Count: 2})
	c.ChatCommand(`summon minecraft:zombie_villager 1.5 64 0.5 {NoAI:1b,VillagerData:{profession:"minecraft:librarian",level:3,type:"minecraft:desert"},Xp:42,Offers:{Recipes:[{buy:{id:"minecraft:emerald",count:10},sell:{id:"minecraft:paper",count:1},maxUses:16,priceMultiplier:0.2f}]}}`)
	zombieType, _ := registry.EntityTypeID("minecraft:zombie_villager")
	id := c.AwaitSpawn(zombieType).EntityID
	c.Settle()
	c.UseOn(id)
	c.Settle()
	if got := srv.InventorySlot(name, player.HotbarSlot).Count; got != 2 {
		t.Fatalf("without weakness, %d apples remain, want 2", got)
	}
	c.ChatCommand(`effect give @e[type=minecraft:zombie_villager] minecraft:weakness 60`)
	c.Settle()
	c.UseOn(id)
	c.AwaitEntityEvent(id, protocol.EntityStatusZombieVillagerCure)
	c.Settle()
	if got := srv.InventorySlot(name, player.HotbarSlot).Count; got != 1 {
		t.Fatalf("after starting the cure, %d apples remain, want 1", got)
	}
	servertest.RunTicks(t, srv, 1)
	curing := false
	c.SettleAndCollect(func(pid int32, data []byte) {
		if pid != protocol.IDPlayEntityMetadata {
			return
		}
		r := protocol.NewReader(data)
		if r.ReadVarInt() != id {
			return
		}
		for r.Err() == nil {
			index := r.ReadU8()
			if index == protocol.MetadataEnd {
				break
			}
			kind := r.ReadVarInt()
			if index == protocol.MetadataIndexZombieVillagerCuring && kind == protocol.MetadataBoolean {
				curing = r.ReadBool()
			} else {
				servertest.SkipBodyEntry(c, r, kind)
			}
		}
	})
	if !curing {
		t.Fatal("the client was not told the zombie villager is curing")
	}
	// Shorten the saved countdown; the rest of the conversion uses the real tick path.
	c.ChatCommand(`data merge entity @e[type=minecraft:zombie_villager,limit=1] {ConversionTime:2}`)
	c.Settle()
	servertest.RunTicks(t, srv, 2)
	villager := c.AwaitSpawn(villagerType(t)).EntityID
	var profession, level int32
	curedSound := false
	c.SettleAndCollect(func(pid int32, data []byte) {
		r := protocol.NewReader(data)
		if pid == protocol.IDPlayWorldEvent && r.ReadI32() == protocol.WorldEventZombieVillagerCured {
			curedSound = true
		}
		if pid != protocol.IDPlayEntityMetadata || r.ReadVarInt() != villager {
			return
		}
		for r.Err() == nil {
			index := r.ReadU8()
			if index == protocol.MetadataEnd {
				break
			}
			kind := r.ReadVarInt()
			if index == protocol.MetadataIndexVillagerVillagerData && kind == protocol.MetadataVillagerData {
				r.ReadVarInt()
				profession, level = r.ReadVarInt(), r.ReadVarInt()
			} else {
				servertest.SkipBodyEntry(c, r, kind)
			}
		}
	})
	if profession != professionLibrarian || level != 3 {
		t.Fatalf("cured villager profession %d level %d, want librarian level 3", profession, level)
	}
	if !curedSound {
		t.Error("no conversion world event 1027")
	}
	if got := openedSpecialPrice(t, c, villager); got != -25 {
		t.Errorf("special price = %d, want -25 from cure reputation 125", got)
	}
	if !srv.AdvancementObtained(name, "minecraft:story/cure_zombie_villager", "cured_zombie") {
		t.Error("the cure advancement was not awarded")
	}
}

func TestASavedZombieCureFinishesWithoutItsPlayer(t *testing.T) {
	t.Parallel()
	const name = "AbsentHealer"
	srv, c, _ := servertest.JoinAs(t, servertest.HostileTestConfig(), name, servertest.OpsListing(t, name))
	servertest.HoldTicks(t, srv)
	c.MoveToGround(0.5, 64, 0.5, true)
	c.ChatCommand(`summon minecraft:zombie_villager 1.5 64 0.5 {NoAI:1b,Silent:1b,ConversionTime:1,ConversionPlayer:[I;0,0,0,1],VillagerData:{profession:"minecraft:librarian",level:1,type:"minecraft:plains"},Offers:{Recipes:[{buy:{id:"minecraft:emerald",count:10},sell:{id:"minecraft:paper",count:1},maxUses:16,priceMultiplier:0.2f}]}}`)
	c.Settle()
	servertest.RunTicks(t, srv, 1)
	villager := c.AwaitSpawn(villagerType(t)).EntityID
	sounded := false
	c.SettleAndCollect(func(pid int32, data []byte) {
		if pid == protocol.IDPlayWorldEvent && protocol.NewReader(data).ReadI32() == protocol.WorldEventZombieVillagerCured {
			sounded = true
		}
	})
	if sounded {
		t.Error("a silent zombie played its conversion event")
	}
	if got := openedSpecialPrice(t, c, villager); got != 0 {
		t.Errorf("special price for a bystander = %d, want 0", got)
	}
	if srv.AdvancementObtained(name, "minecraft:story/cure_zombie_villager", "cured_zombie") {
		t.Error("a bystander was awarded the cure advancement")
	}
}

// TestARiderIsToldTheCureEffectsOfTheirZombieVillager: the cure's Weakness
// removal and Strength go to the player riding the zombie villager.
// vanilla: LivingEntity.sendEffectToPassengers
func TestARiderIsToldTheCureEffectsOfTheirZombieVillager(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.HostileTestConfig(), servertest.OpsListing(t, "Healer", "Rider"))
	healer := connect().Join("Healer")
	rider := connect().Join("Rider")
	servertest.HoldTicks(t, srv)
	srv.SetDayTime(18000)
	srv.SetPlayerGameMode("Healer", protocol.GameModeSurvival)
	healer.MoveToGround(0.5, 64, 0.5, true)
	rider.MoveToGround(3.5, 64, 0.5, true)
	srv.GiveSlot("Healer", player.HotbarSlot, protocol.ItemStack{ItemID: registry.MustItem("minecraft:golden_apple"), Count: 1})
	healer.ChatCommand(`summon minecraft:zombie_villager 1.5 64 0.5 {NoAI:1b}`)
	zombieType, _ := registry.EntityTypeID("minecraft:zombie_villager")
	id := healer.AwaitSpawn(zombieType).EntityID
	rider.AwaitSpawn(zombieType)
	healer.ChatCommand(`effect give @e[type=minecraft:zombie_villager] minecraft:weakness 60`)
	healer.Settle()
	rider.ChatCommand("ride @s mount @e[type=minecraft:zombie_villager,limit=1]")
	rider.Settle()

	healer.UseOn(id)
	healer.AwaitEntityEvent(id, protocol.EntityStatusZombieVillagerCure)
	strength, weakness := false, false
	rider.SettleAndCollect(func(pid int32, data []byte) {
		r := protocol.NewReader(data)
		switch pid {
		case protocol.IDPlayUpdateMobEffect:
			if r.ReadVarInt() == id && r.ReadVarInt() == registry.MobEffectStrength {
				strength = true
			}
		case protocol.IDPlayRemoveMobEffect:
			if r.ReadVarInt() == id && r.ReadVarInt() == registry.MobEffectWeakness {
				weakness = true
			}
		}
	})
	if !strength || !weakness {
		t.Fatalf("the rider was told strength added %v, weakness removed %v; want both", strength, weakness)
	}
}
