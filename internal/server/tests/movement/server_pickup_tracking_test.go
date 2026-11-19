package movement

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/entity"
)

// TestItemPickupAnimationReachesOnlyItsTrackers lets one player take an item
// while a second stands beside it and a third stands 200 blocks away: the
// Take Item Entity packet reaches the collector and the near player, and
// the far one, who never saw the item, gets none.
// vanilla: LivingEntity.take, ChunkMap.sendToTrackingPlayers
func TestItemPickupAnimationReachesOnlyItsTrackers(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.BlockTestConfig())

	far := connect().Join("Far")
	far.MoveTo(200.5, 64, 0.5, 0, 0)
	far.Settle()

	near := connect().Join("Near")
	near.MoveTo(10.5, 64, 0.5, 0, 0)
	near.Settle()

	collector := connect().Join("Collector")
	collector.Settle()
	near.Settle()
	far.Settle()

	if got := srv.PlayerPosition("Far"); got.X < 100 {
		t.Fatalf("the far player stands at x=%v, want it moved away", got.X)
	}

	srv.SpawnItemEntity(0.5, 64+entity.ItemHalfSize, 0.5, "minecraft:dirt", 3)
	collector.ExpectDroppedItem()
	near.ExpectDroppedItem()

	for _, c := range []*servertest.Client{collector, near} {
		if !c.Pump(servertest.ExpectTimeout, nil, func(id int32, _ []byte) bool { return id == protocol.IDPlayPickupItem }) {
			t.Errorf("a player who sees the item was sent no Take Item Entity packet")
		}
	}
	got := false
	far.SettleAndCollect(func(id int32, _ []byte) { got = got || id == protocol.IDPlayPickupItem })
	if got {
		t.Error("the far player was sent a Take Item Entity packet for an item out of its view")
	}
}
