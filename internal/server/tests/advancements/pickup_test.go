package advancements

import (
	"testing"
	"time"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// The triggers of a thrown item changing hands: the allay's delivery to its
// player or onto a note block, and the piglin taking the gold a player threw.
// vanilla: ServerPlayer.onItemPickup, LivingEntity.onItemPickup, AllayAi.throwItem

// pickupWorld joins a survival player standing at (0.5, 64, 0.5) holding
// count of item, with the loop held.
func pickupWorld(t *testing.T, item string, count int32) (*server.Server, *servertest.Client) {
	t.Helper()
	srv, c := servertest.Join(t, servertest.MobTestConfig(), "Thrower", func(s *server.Server) {
		s.SetKeepAlive(time.Hour, time.Hour)
	})
	servertest.HoldTicks(t, srv)
	c.MoveToGround(0.5, 64, 0.5, true)
	srv.GiveSlot("Thrower", player.HotbarSlot, protocol.ItemStack{ItemID: servertest.MustItemID(t, item), Count: count})
	c.Settle()
	return srv, c
}

// givenAllay spawns an allay beside the player and hands it the held item.
func givenAllay(t *testing.T, srv *server.Server, c *servertest.Client) {
	t.Helper()
	id := srv.SpawnMob("minecraft:allay", 2.5, 65, 0.5)
	c.UseOn(id)
	c.Settle()
	if srv.MobEquipment(id, "mainhand").Empty() {
		t.Fatal("the allay took nothing from the player")
	}
}

// TestAnAllayDeliveringToItsPlayerObtainsAllayDeliverItemToPlayer: the
// player picking up what the allay threw is a thrown_item_picked_up_by_player
// by an allay.
func TestAnAllayDeliveringToItsPlayerObtainsAllayDeliverItemToPlayer(t *testing.T) {
	t.Parallel()
	srv, c := pickupWorld(t, "minecraft:diamond", 1)
	givenAllay(t, srv, c)
	srv.SpawnItemEntity(5.5, 64, 0.5, "minecraft:diamond", 1)
	servertest.UntilTicks(t, srv, 400, "allay_deliver_item_to_player", func() bool {
		return srv.AdvancementObtained("Thrower", "minecraft:husbandry/allay_deliver_item_to_player", "allay_deliver_item_to_player")
	})
}

// TestAnAllayDroppingCakeOnANoteBlockObtainsAllayDeliverCakeToNoteBlock:
// the allay's throw at a played note block fires allay_drop_item_on_block
// for its player, whose criterion wants a cake on a note block.
func TestAnAllayDroppingCakeOnANoteBlockObtainsAllayDeliverCakeToNoteBlock(t *testing.T) {
	t.Parallel()
	const noteX, noteZ = 3, 2
	srv, c := pickupWorld(t, "minecraft:cake", 1)
	srv.SetBlock(noteX, 64, noteZ, registry.BlockStates["minecraft:note_block"])
	givenAllay(t, srv, c)
	c.UseBlock(noteX, 64, noteZ)
	c.Settle()
	// Away from the note block, so the allay does not deliver to the player.
	c.MoveToGround(-5.5, 64, 0.5, true)
	c.Settle()
	srv.SpawnItemEntity(0.5, 64, -4.5, "minecraft:cake", 1)
	servertest.UntilTicks(t, srv, 400, "allay_deliver_cake_to_note_block", func() bool {
		return srv.AdvancementObtained("Thrower", "minecraft:husbandry/allay_deliver_cake_to_note_block", "allay_deliver_cake_to_note_block")
	})
	if srv.AdvancementObtained("Thrower", "minecraft:husbandry/allay_deliver_item_to_player", "allay_deliver_item_to_player") {
		t.Error("the cake was delivered to the player, not the note block")
	}
}

// TestAGoldIngotThrownToAPiglinObtainsDistractPiglin: an adult piglin
// picking up the gold ingot a player without gold armor threw is a
// thrown_item_picked_up_by_entity for the thrower.
func TestAGoldIngotThrownToAPiglinObtainsDistractPiglin(t *testing.T) {
	t.Parallel()
	srv, c := pickupWorld(t, "minecraft:gold_ingot", 1)
	p := srv.SpawnMob("minecraft:piglin", 0.5, 64, 3.5)
	c.DropKey(false)
	c.Settle()
	servertest.UntilTicks(t, srv, 120, "the piglin taking the thrown ingot", func() bool {
		return srv.MobOffHand(p).Count == 1
	})
	if !srv.AdvancementObtained("Thrower", "minecraft:nether/distract_piglin", "distract_piglin") {
		t.Error("the piglin took the thrown ingot and distract_piglin is not obtained")
	}
}
