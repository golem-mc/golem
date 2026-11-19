package mobs

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

func TestSummonedPiglinLootPickupDefault(t *testing.T) {
	for _, age := range []string{"adult", "baby"} {
		for _, tc := range []struct {
			name, tag, flag string
			pickup          bool
		}{
			{"omitted", "", "1b", true},
			{"enabled", ",CanPickUpLoot:1b", "1b", true},
			{"disabled", ",CanPickUpLoot:0b", "0b", false},
		} {
			t.Run(age+"/"+tc.name, func(t *testing.T) {
				t.Parallel()
				c, _, srv := barterWorld(t)
				baby := ""
				if age == "baby" {
					baby = ",IsBaby:1b"
				}
				c.ChatCommand("summon minecraft:piglin 5.5 64 0.5 {IsImmuneToZombification:1b" + baby + tc.tag + "}")
				p := c.ExpectMobSpawn(protocol.EntityTypePiglin)
				name := servertest.EntityDisplayComponent(barterPiglin, "entity.minecraft.piglin", p.Uuid)
				c.ExpectCommandFeedbackText("commands.summon.success", name)
				c.ChatCommand("data get entity @e[type=minecraft:piglin,limit=1] CanPickUpLoot")
				c.ExpectCommandFeedbackText("commands.data.entity.query", name, servertest.NBTArg(tc.flag))
				c.ChatCommand(`summon item 5.5 64 0.5 {Item:{id:"minecraft:gold_ingot",count:1}}`)
				c.Settle()
				if !tc.pickup {
					servertest.RunTicks(t, srv, 40)
					if !srv.MobOffHand(p.EntityID).Empty() || srv.ItemCount() != 1 {
						t.Fatal("piglin with CanPickUpLoot:0b took the ingot")
					}
					return
				}
				servertest.UntilTicks(t, srv, 40, "the summoned piglin taking the ingot", func() bool {
					held := srv.MobOffHand(p.EntityID)
					return held.ItemID == int32(registry.ItemGoldIngot) && held.Count == 1
				})
				if n := srv.ItemCount(); n != 0 {
					t.Fatalf("%d items on the ground after pickup, want none", n)
				}
			})
		}
	}
}
