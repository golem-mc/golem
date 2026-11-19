package entities

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// vanilla: BlockAttachedEntity.tick, HangingEntity.survives
func TestPaintingSurvivalWaitsForEntityTicks(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, change string
	}{
		{"support", "setblock 5 64 4 air"},
		{"collision", "setblock 5 64 5 stone"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Op", servertest.OpsListing(t, "Op"))
			servertest.HoldTicks(t, srv)
			c.ChatCommand("setblock 5 64 4 stone")
			servertest.AwaitSystemChatSaying(t, c, "commands.setblock.success")
			c.ChatCommand(`summon painting 5 64 5 {variant:"minecraft:kebab",facing:0b}`)
			spawn := c.AwaitSpawn(protocol.EntityTypePainting)
			c.AwaitMetadataFor(spawn.EntityID)
			servertest.AwaitSystemChatSaying(t, c, "commands.summon.success")
			servertest.RunTicks(t, srv, 37)
			c.ChatCommand("tick freeze")
			c.ExpectTickingState(20, true)
			c.ExpectCommandFeedback("commands.tick.status.frozen")
			c.ChatCommand(tc.change)
			servertest.AwaitSystemChatSaying(t, c, "commands.setblock.success")

			// Frozen loop ticks must neither break it nor advance its counter.
			noBreak := func(id int32, _ []byte) bool {
				return id == protocol.IDPlayRemoveEntities || id == protocol.IDPlaySpawnEntity
			}
			c.ExpectNoneWithinTicks(srv, 110, noBreak)
			if n := srv.PaintingCount(); n != 1 {
				t.Fatalf("frozen painting count = %d, want 1", n)
			}
			c.ChatCommand("tick unfreeze")
			c.ExpectTickingState(20, false)
			c.ExpectCommandFeedback("commands.tick.status.running")
			c.ExpectNoneWithinTicks(srv, 63, noBreak)
			servertest.RunTicks(t, srv, 1)
			c.ExpectPaintingRemoval(t, spawn.EntityID)
			drop := c.AwaitSpawn(protocol.EntityTypeItem)
			meta := c.AwaitMetadataFor(drop.EntityID)
			item := servertest.MetaOf(meta, protocol.MetadataIndexItemSlot)
			if item == nil || item.SlotVal.ItemID != servertest.PaintingItem(t) || item.SlotVal.Count != 1 {
				t.Errorf("survival check drop = %+v, want one painting", item)
			}
			if n := srv.PaintingCount(); n != 0 {
				t.Errorf("painting count after tick 101 = %d, want 0", n)
			}
			c.Disconnect()
		})
	}
}
