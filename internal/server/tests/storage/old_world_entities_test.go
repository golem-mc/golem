package storage

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"
)

// TestAVanilla1_16_5WorldLoadsItsEntities opens a chunk vanilla 1.16.5 wrote
// with two boats, a TNT minecart, a villager and an armor stand in it: the
// boats are the oak and birch boats of today, and all of them are there.
//
// The fixture is chunk (0,0) of a superflat 1.16.5 world with these commands run:
//
//	summon minecraft:boat 2.5 5 2.5 {Type:"birch"}
//	summon minecraft:boat 6.5 5 2.5 {Type:"oak"}
//	summon minecraft:tnt_minecart 10.5 5 2.5 {TNTFuse:-1}
//	summon minecraft:villager 14.5 5 2.5 {NoAI:1b}
//	summon minecraft:armor_stand 2.5 5 6.5 {ArmorItems:[{},{},{},{id:"minecraft:leather_helmet",Count:1b}]}
//
// vanilla: BoatSplitFix, EntityFieldsRenameFix, VillagerSetVillagerDataFinalized
func TestAVanilla1_16_5WorldLoadsItsEntities(t *testing.T) {
	t.Parallel()
	cfg := servertest.MobTestConfig()
	cfg.LevelName, _ = oldWorld(t, "vanilla-1.16.5-entities-r.0.0.mca")
	const name = "Keeper"
	srv, c := servertest.Join(t, cfg, name, servertest.OpsListing(t, name))
	servertest.HoldTicks(t, srv)
	release := srv.HoldChunk(0, 0)
	defer release()
	for _, selector := range []string{
		"birch_boat", "oak_boat", "tnt_minecart", "villager", "armor_stand",
	} {
		c.ChatCommand("execute if entity @e[type=minecraft:" + selector + "]")
		servertest.AwaitSystemChatSaying(t, c, "commands.execute.conditional.pass")
	}
}
