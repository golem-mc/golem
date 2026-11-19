package entities

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
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

// TestAMinecartIsShownAgainToAPlayerBackFromAnotherDimension sends a player
// standing beside a cart through the Nether and back to the same spot. A
// second player keeps the cart's chunk, and so the cart, ticking meanwhile.
func TestAMinecartIsShownAgainToAPlayerBackFromAnotherDimension(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.BlockTestConfig())
	connect().Join("Holder")
	c := connect().Join("Crosser")
	cart := cartBesideWatcher(t, srv, c)

	srv.MovePlayerToDimension("Crosser", world.NetherName, 88, 40, 88)
	srv.MovePlayerToDimension("Crosser", world.DimensionName, 0.5, 64, 0.5)

	if !c.ReadTicks(srv, 20, spawnOf(cart)) {
		t.Fatal("the player back from the Nether was not shown the cart again")
	}
}

// TestAMinecartCarryingAPlayerIsShownOutToThePlayerRange: a cart is seen from
// 128 blocks, a player's body from 160, so a cart with a rider is seen as far
// as the rider, and a watcher between the two sees the cart under the body.
// vanilla: ChunkMap.TrackedEntity.getEffectiveRange
func TestAMinecartCarryingAPlayerIsShownOutToThePlayerRange(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	cfg.GameMode = "creative"
	cfg.ViewDistance = 10
	srv, connect := servertest.Start(t, cfg)
	rider := connect().Join("Rider")
	rider.MoveToGround(0.5, 64, 0.5, true)
	rider.Settle()
	srv.GiveSlot("Rider", player.HotbarSlot,
		protocol.ItemStack{ItemID: servertest.MustItemID(t, "minecraft:minecart"), Count: 1})
	servertest.CartRails(t, srv, 3, 6)
	far := connect()
	far.ViewAsk = 10
	watcher := far.Join("Watcher")
	watcher.MoveToGround(140.5, 64, 0.5, true)
	watcher.Settle()
	cartID := srv.SpawnMinecart(3.5, 64.0625, 1.5)
	if cartID == 0 {
		t.Fatal("SpawnMinecart came to nothing")
	}

	rider.MoveTo(3.5, 64, 2.5, 0, 60)
	rider.Settle()
	rider.UseOn(cartID)
	if riders := servertest.AwaitPassengersFor(rider, cartID); len(riders) != 1 {
		t.Fatalf("passengers = %v, want the rider aboard", riders)
	}
	if !watcher.ReadTicks(srv, 400, spawnOf(cartID)) {
		t.Error("a watcher 140 blocks out was never shown the cart its rider sits in")
	}
}
