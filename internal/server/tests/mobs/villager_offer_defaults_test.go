package mobs

import (
	"fmt"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/nbt"
)

// vanilla: Villager.restock, Villager.catchUpDemand, MerchantOffer.CODEC
func TestVillagerRestockOmitsOfferDefaults(t *testing.T) {
	for _, tc := range []struct {
		name        string
		lastRestock int64
		demand      int
	}{
		{"restock", 0, 0},
		{"catch_up", -13000, 8},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := servertest.MobTestConfig()
			cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
			srv, c := servertest.Join(t, cfg, "Villager", servertest.OpsListing(t, "Villager"))
			servertest.HoldTicks(t, srv)
			srv.SetDayTime(3000)
			srv.SetBlock(3, 64, 0, stateOf(t, "minecraft:lectern", nil))
			c.ChatCommand(fmt.Sprintf(`summon villager 2.5 64 0.5 {Xp:1,LastRestock:%dL,VillagerData:{profession:"minecraft:librarian",level:1,type:"minecraft:plains"},Brain:{memories:{"minecraft:job_site":{value:{dimension:"minecraft:overworld",pos:[I;3,64,0]}}}},Offers:{Recipes:[{buy:{id:"minecraft:emerald",count:1},sell:{id:"minecraft:bread",count:6},uses:2d,maxUses:4b,demand:%dL,specialPrice:0s,rewardExp:1s,xp:1b,priceMultiplier:0d}]}}`, tc.lastRestock, tc.demand))
			spawn := c.AwaitSpawn(protocol.EntityTypeVillager)
			c.ExpectFeedbackLine("commands.summon.success")
			seen := servertest.VariantSeen{EntityID: spawn.EntityID, Meta: map[uint8][]int32{}}
			// WorkAtPoi rolls at 300-tick intervals; allow four attempts.
			for range 120 {
				advanceVillager(t, srv, c, 10, &seen)
				offers, _ := srv.VillagerSaved(spawn.EntityID)
				list, _ := nbt.Get[nbt.List](offers, "Recipes")
				if len(list.Items) == 1 {
					recipe, ok := nbt.CompoundView(list.Items[0])
					if !ok {
						t.Fatalf("recipe is %T, want compound", list.Items[0])
					}
					uses, _ := nbt.IntValue(recipe["uses"])
					if uses == 0 {
						break
					}
				}
			}
			c.ChatCommand(`data get entity @e[type=villager,limit=1] Offers.Recipes[0]`)
			c.ExpectCommandFeedbackText("commands.data.entity.query",
				servertest.EntityDisplayComponent("minecraft:villager", "entity.minecraft.villager.librarian", spawn.Uuid),
				servertest.NBTArg(`{buy:{id:"minecraft:emerald",count:1},sell:{id:"minecraft:bread",count:6}}`))
		})
	}
}
