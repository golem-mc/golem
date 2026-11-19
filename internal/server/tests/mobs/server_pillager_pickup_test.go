package mobs

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

func TestSummonedPillagerLootPickup(t *testing.T) {
	for _, tag := range []string{"{NoAI:0b}", "{NoAI:0b,CanPickUpLoot:0b}"} {
		t.Run(tag, func(t *testing.T) {
			t.Parallel()
			cfg := servertest.MobTestConfig()
			cfg.GameMode = "creative"
			srv, c := servertest.Join(t, cfg, "Observer", servertest.OpsListing(t, "Observer"))
			servertest.HoldTicks(t, srv)
			c.ChatCommand("summon minecraft:pillager 5.5 64 5.5 " + tag)
			p := c.ExpectMobSpawn(protocol.EntityTypePillager)
			name := servertest.EntityDisplayComponent(pillagerKind, "entity.minecraft.pillager", p.Uuid)
			c.ExpectCommandFeedbackText("commands.summon.success", name)
			c.ChatCommand("data get entity @e[type=minecraft:pillager,limit=1] CanPickUpLoot")
			c.ExpectCommandFeedbackText("commands.data.entity.query", name, servertest.NBTArg("1b"))
			c.ChatCommand(`summon item 5.5 64 5.5 {Item:{id:"minecraft:iron_helmet",count:1}}`)
			c.Settle()
			servertest.RunTicks(t, srv, 40)
			if !srv.MobEquipment(p.EntityID, "head").Empty() || srv.ItemCount() != 1 {
				t.Fatal("pillager took an iron helmet; vanilla only accepts banners")
			}
			c.ChatCommand(`summon item 5.5 64 5.5 {Item:{id:"minecraft:white_banner",count:1}}`)
			c.Settle()
			servertest.UntilTicks(t, srv, 40, "the pillager holding the banner", func() bool {
				return srv.MobEquipment(p.EntityID, "mainhand").ItemID == int32(registry.ItemWhiteBanner)
			})
			c.Settle()
			c.ChatCommand("data get entity @e[type=minecraft:pillager,limit=1] equipment.mainhand.id")
			c.ExpectCommandFeedbackText("commands.data.entity.query", name, servertest.NBTArg(`"minecraft:white_banner"`))
			if n := srv.ItemCount(); n != 1 {
				t.Fatalf("%d items left after banner pickup, want the helmet", n)
			}
		})
	}
}
