package entities

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// A watcher who leaves, or whose Respawn clears its world, is dropped from
// every cart's record, so the tick pairs the cart with it again once it is
// back in range.
// vanilla: ChunkMap.removeEntity, TrackedEntity.removePlayer

// spawnOf picks out the Spawn Entity of the entity ID given.
func spawnOf(id int32) func(packetID int32, data []byte) bool {
	return func(packetID int32, data []byte) bool {
		return packetID == protocol.IDPlaySpawnEntity && protocol.NewReader(data).ReadVarInt() == id
	}
}

// cartBesideWatcher stands a cart beside a joined player and waits for the
// player to be shown it.
func cartBesideWatcher(t *testing.T, srv *server.Server, c *servertest.Client) int32 {
	t.Helper()
	c.Settle()
	cart := srv.SpawnMinecart(2.5, 64, 2.5)
	if cart == 0 {
		t.Fatal("the cart never stood up")
	}
	if !c.ReadTicks(srv, 20, spawnOf(cart)) {
		t.Fatal("the player beside the cart was not shown it")
	}
	return cart
}

// TestAMinecartIsShownAgainToAPlayerWhoRejoinsBesideIt has a player leave
// beside a cart they were shown and join again where they left. A second
// player keeps the cart's chunk loaded meanwhile, so the cart is the same
// body, under the same ID, when the first comes back.
func TestAMinecartIsShownAgainToAPlayerWhoRejoinsBesideIt(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.BlockTestConfig())
	connect().Join("Holder")
	c := connect().Join("Watcher")
	cart := cartBesideWatcher(t, srv, c)
	c.Disconnect()

	back := connect()
	back.Login("Watcher")
	back.Configure()
	shown := spawnOf(cart)
	var inJoin bool
	back.ReadJoin(func(id int32, data []byte) {
		inJoin = inJoin || shown(id, data)
	})
	back.MarkLoaded()
	if !inJoin && !back.ReadTicks(srv, 20, shown) {
		t.Fatal("the player who rejoined beside the cart was not shown it again")
	}
}
