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
	servertest.HoldTicks(t, srv)
	clients := []*servertest.Client{first}
	ids := []int32{firstID}
	for i := 1; i < 9; i++ {
		c := servertest.DialServer(t, srv)
		ids = append(ids, c.JoinAs(fmt.Sprintf("Crowd%d", i)))
		clients = append(clients, c)
	}
	// Players 0 and 1 straddle the x=16 border; the rest stand well clear.
	clients[0].MoveToGround(15.5, 64, 0.5, true)
	clients[1].MoveToGround(16.5, 64, 0.5, true)
	for i := 2; i < 9; i++ {
		clients[i].MoveToGround(float64(i), 64, 8.5, true)
	}
	for _, c := range clients {
		c.Settle()
	}
	srv.SpawnOwnedItemEntity(16, 64, 0.5, "minecraft:stone", 1, "Crowd1")
	r := protocol.NewReader(clients[1].ExpectWithinTicks(srv, 3, protocol.IDPlayPickupItem))
	r.ReadVarInt()
	if got := r.ReadVarInt(); got != ids[1] {
		t.Fatalf("collector %d, want the owner %d", got, ids[1])
	}
}
