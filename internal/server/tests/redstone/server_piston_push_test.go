package redstone

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"
)

// The bodies a moving piston runs into ride along with it: the head's plate
// carries what stands in its cell out ahead of it, and a slime block lifts
// what stands on it.
// vanilla: PistonMovingBlockEntity.moveCollidedEntities

// TestAPistonHeadPushesAnItemInItsCell is the same extension over a stack of
// items lying in the head's cell.
func TestAPistonHeadPushesAnItemInItsCell(t *testing.T) {
	t.Parallel()
	srv, actor := servertest.Join(t, servertest.BlockTestConfig(), "Actor", servertest.OpsListing(t, "Actor"))

	servertest.Setblock(t, actor, 2, 64, 0, "piston[facing=east]")
	actor.Settle()
	srv.SpawnItemEntity(3.5, 64.13, 0.5, "minecraft:stone", 1)
	servertest.Setblock(t, actor, 2, 64, 1, "redstone_block")
	servertest.AwaitTicks(t, srv, 6)

	items := srv.ItemsInDimension("minecraft:overworld")
	if len(items) != 1 {
		t.Fatalf("%d items, want the one stack", len(items))
	}
	if x := items[0].X; x < 4.1 {
		t.Errorf("the item lay at x %v after the extension, want in front of the head (x >= 4.1)", x)
	}
}

// TestASlimeBlockLeavesAPlayerStandingOnItToTheClient extends a piston upward
// under a slime block with a player on top; the server does not teleport the
// player, which would cancel the client's own launch.
func TestASlimeBlockLeavesAPlayerStandingOnItToTheClient(t *testing.T) {
	t.Parallel()
	srv, actor := servertest.Join(t, servertest.BlockTestConfig(), "Actor", servertest.OpsListing(t, "Actor"))

	servertest.Setblock(t, actor, 2, 64, 0, "piston[facing=up]")
	servertest.Setblock(t, actor, 2, 65, 0, "slime_block")
	actor.MoveTo(2.5, 66, 0.5, 0, 0)
	actor.Settle()
	servertest.Setblock(t, actor, 3, 64, 0, "redstone_block")
	servertest.AwaitTicks(t, srv, 6)
	actor.Settle()

	if got := srv.PlayerPosition("Actor"); got.Y != 66 {
		t.Errorf("the player stood at y %v after the slime block rose, want the reported 66", got.Y)
	}
}
