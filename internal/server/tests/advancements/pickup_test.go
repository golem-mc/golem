package advancements

import (
	"testing"
	"time"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
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
