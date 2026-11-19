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
