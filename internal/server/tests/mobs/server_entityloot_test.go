package mobs

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// TestEveryEntityLootTableIsKnown: a killed warden drops its sculk catalyst,
// and /loot rolls the copper golem's, zombie horse's and camel husk's tables.
// vanilla: LivingEntity.dropFromLootTable, EntityType.getDefaultLootTable
func TestEveryEntityLootTableIsKnown(t *testing.T) {
	t.Parallel()
	srv, c, _ := servertest.JoinAs(t, servertest.MobTestConfig(), "Looter", servertest.OpsListing(t, "Looter"))
	for table, item := range map[string]string{
		"copper_golem": "minecraft:copper_ingot",
		"zombie_horse": "minecraft:rotten_flesh",
		"camel_husk":   "minecraft:rotten_flesh",
	} {
		c.ChatCommand("loot give @s loot minecraft:entities/" + table)
		updates, chats := servertest.AwaitSlotUpdates(t, c, 1)
		if len(chats) == 0 {
			c.ExpectPlay(protocol.IDPlaySystemChat)
		}
		if got := updates[0].Stack; got.ItemID != servertest.MustItem(item) || got.Count < 1 {
			t.Errorf("entities/%s gave item %d x%d, want at least one %s", table, got.ItemID, got.Count, item)
		}
	}

	servertest.HoldTicks(t, srv)
	srv.RunLine("summon minecraft:warden 5.5 64 5.5 {NoAI:1b}")
	c.ExpectMobSpawn(protocol.EntityTypeWarden)
	srv.RunLine("kill @e[type=minecraft:warden]")
	servertest.RunTicks(t, srv, 25)
	servertest.ExpectOneDroppedItem(t, srv, "minecraft:sculk_catalyst")
}
