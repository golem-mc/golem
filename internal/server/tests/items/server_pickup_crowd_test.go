package items

import (
	"fmt"
	"testing"
	"time"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// TestCrowdedPickupGoesToTheOwner: with more players than the pickup pass
// walks directly, a drop on a chunk border only its owner may take is taken by
// the owner, though another player reaches it too.
// vanilla: ItemEntity.playerTouch
func TestCrowdedPickupGoesToTheOwner(t *testing.T) {
	t.Parallel()
	srv, first, firstID := servertest.JoinAs(t, servertest.MobTestConfig(), "Crowd0", func(s *server.Server) {
		s.SetKeepAlive(time.Hour, time.Hour)
	})
	clients := []*servertest.Client{first}
	ids := []int32{firstID}
	for i := 1; i < 9; i++ {
		c := servertest.DialServer(t, srv)
		ids = append(ids, c.JoinAs(fmt.Sprintf("Crowd%d", i)))
		clients = append(clients, c)
	}
	servertest.HoldTicks(t, srv)
	// Players 0 and 1 straddle the x=16 border; the rest stay at spawn.
	clients[0].MoveToGround(15.5, 64, 0.5, true)
	clients[1].MoveToGround(16.5, 64, 0.5, true)
	for _, c := range clients[:2] {
		c.Settle()
	}
	words := servertest.UuidWords(protocol.OfflineUUID("Crowd1"))
	srv.RunLine(fmt.Sprintf(`summon minecraft:item 16.0 64 0.5 {Item:{id:"minecraft:stone",count:1},PickupDelay:0s,Owner:[I;%d,%d,%d,%d]}`,
		int32(words[0]), int32(words[1]), int32(words[2]), int32(words[3])))
	drop := servertest.ParseItemSpawn(clients[1].ExpectWithinTicks(srv, 3, protocol.IDPlaySpawnEntity))
	if drop.EntityID == 0 || drop.X != 16 {
		t.Fatalf("summoned drop = %+v, want an item at x=16", drop)
	}
	collected, collector, count := servertest.ReadPickupItem(clients[1], clients[1].ExpectWithinTicks(srv, 3, protocol.IDPlayPickupItem))
	if collected != drop.EntityID || collector != ids[1] || count != 1 {
		t.Fatalf("pickup of %d by %d (count %d), want %d by the owner %d and 1", collected, collector, count, drop.EntityID, ids[1])
	}
}
