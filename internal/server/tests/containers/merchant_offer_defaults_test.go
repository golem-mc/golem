package containers

import (
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// vanilla: MerchantOffer.CODEC, MerchantResultSlot.onTake
func TestTradedVillagerRecipeOmitsCodecDefaults(t *testing.T) {
	cfg := servertest.MobTestConfig()
	cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
	srv, c := servertest.Join(t, cfg, "Buyer", servertest.OpsListing(t, "Buyer"))
	servertest.HoldTicks(t, srv)
	w := &servertest.WindowedClient{Client: c, WantBlockEntityType: -1, States: map[int32]int32{0: 1}}
	c.ChatCommand(`summon villager 1.5 64 0.5 {NoAI:1b,VillagerData:{profession:"minecraft:farmer",level:1,type:"minecraft:plains"},Offers:{Recipes:[{buy:{id:"minecraft:emerald",count:1},sell:{id:"minecraft:bread",count:6},maxUses:12,xp:2,priceMultiplier:0.05f}]}}`)
	spawn := c.AwaitSpawn(protocol.EntityTypeVillager)
	c.ExpectFeedbackLine("commands.summon.success")
	srv.GiveSlot("Buyer", player.HotbarSlot, protocol.ItemStack{ItemID: int32(registry.ItemEmerald), Count: 1})
	if openTrade(w, spawn.EntityID) != 1 {
		t.Fatal("the trade window did not open")
	}
	selectTrade(w, 0)
	w.Click(merchantResult, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{merchantPay0: {}, merchantResult: {}},
		protocol.ItemStack{ItemID: int32(registry.ItemBread), Count: 6})
	w.CloseWindow()
	w.Settle()
	c.ChatCommand(`data get entity @e[type=villager,limit=1] Offers.Recipes[0]`)
	c.ExpectCommandFeedbackText("commands.data.entity.query",
		servertest.EntityDisplayComponent("minecraft:villager", "entity.minecraft.villager.farmer", spawn.Uuid),
		servertest.NBTArg(`{buy:{id:"minecraft:emerald",count:1},sell:{id:"minecraft:bread",count:6},maxUses:12,xp:2,priceMultiplier:0.05f,uses:1}`))
}

// vanilla: MerchantOffer.CODEC, NbtOps.getNumberValue, NbtOps.getBooleanValue
func TestClosingVillagerOfferPreservesNumericCodecValues(t *testing.T) {
	cfg := servertest.MobTestConfig()
	cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
	srv, c := servertest.Join(t, cfg, "Buyer", servertest.OpsListing(t, "Buyer"))
	servertest.HoldTicks(t, srv)
	w := &servertest.WindowedClient{Client: c, WantBlockEntityType: -1, States: map[int32]int32{0: 1}}
	c.ChatCommand(`summon villager 1.5 64 0.5 {NoAI:1b,VillagerData:{profession:"minecraft:farmer",level:1,type:"minecraft:plains"},Offers:{Recipes:[{buy:{id:"minecraft:emerald",count:1b},sell:{id:"minecraft:bread",count:6},uses:3s,maxUses:12b,xp:2s,priceMultiplier:0.25d,rewardExp:0s,demand:2L}]}}`)
	spawn := c.AwaitSpawn(protocol.EntityTypeVillager)
	c.ExpectFeedbackLine("commands.summon.success")
	if openTrade(w, spawn.EntityID) != 1 {
		t.Fatal("the trade window did not open")
	}
	w.CloseWindow()
	w.Settle()
	c.ChatCommand(`data get entity @e[type=villager,limit=1] Offers.Recipes[0]`)
	c.ExpectCommandFeedbackText("commands.data.entity.query",
		servertest.EntityDisplayComponent("minecraft:villager", "entity.minecraft.villager.farmer", spawn.Uuid),
		servertest.NBTArg(`{buy:{id:"minecraft:emerald",count:1},sell:{id:"minecraft:bread",count:6},uses:3,maxUses:12,xp:2,priceMultiplier:0.25f,rewardExp:0b,demand:2}`))
}

// vanilla: ItemCost.CODEC, MerchantResultSlot.onTake
func TestVillagerCostCountsNormalizeWithoutChangingTheTrade(t *testing.T) {
	cfg := servertest.MobTestConfig()
	cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
	srv, c := servertest.Join(t, cfg, "Buyer", servertest.OpsListing(t, "Buyer"))
	servertest.HoldTicks(t, srv)
	w := &servertest.WindowedClient{Client: c, WantBlockEntityType: -1, States: map[int32]int32{0: 1}}
	c.ChatCommand(`summon villager 1.5 64 0.5 {NoAI:1b,VillagerData:{profession:"minecraft:farmer",level:1,type:"minecraft:plains"},Offers:{Recipes:[{buy:{id:"minecraft:emerald",count:2b},buyB:{id:"minecraft:book",count:3b},sell:{id:"minecraft:bread",count:6},maxUses:12}]}}`)
	spawn := c.AwaitSpawn(protocol.EntityTypeVillager)
	c.ExpectFeedbackLine("commands.summon.success")
	if openTrade(w, spawn.EntityID) != 1 {
		t.Fatal("the trade window did not open")
	}
	w.CloseWindow()
	w.Settle()
	c.ChatCommand(`data get entity @e[type=villager,limit=1] Offers.Recipes[0]`)
	c.ExpectCommandFeedbackText("commands.data.entity.query",
		servertest.EntityDisplayComponent("minecraft:villager", "entity.minecraft.villager.farmer", spawn.Uuid),
		servertest.NBTArg(`{buy:{id:"minecraft:emerald",count:2},buyB:{id:"minecraft:book",count:3},sell:{id:"minecraft:bread",count:6},maxUses:12}`))
	srv.GiveSlot("Buyer", player.HotbarSlot, protocol.ItemStack{ItemID: int32(registry.ItemEmerald), Count: 2})
	srv.GiveSlot("Buyer", player.HotbarSlot+1, protocol.ItemStack{ItemID: int32(registry.ItemBook), Count: 3})
	if openTrade(w, spawn.EntityID) != 1 {
		t.Fatal("the trade window did not reopen")
	}
	selectTrade(w, 0)
	w.Click(merchantResult, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{merchantPay0: {}, merchantPay1: {}, merchantResult: {}},
		protocol.ItemStack{ItemID: int32(registry.ItemBread), Count: 6})
	w.CloseWindow()
	w.Settle()
	c.ChatCommand(`data get entity @e[type=villager,limit=1] Offers.Recipes[0].uses`)
	c.ExpectCommandFeedbackText("commands.data.entity.query",
		servertest.EntityDisplayComponent("minecraft:villager", "entity.minecraft.villager.farmer", spawn.Uuid), servertest.NBTArg(`1`))
}
