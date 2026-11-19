package redstone

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"
)

// The bodies a moving piston runs into ride along with it: the head's plate
// carries what stands in its cell out ahead of it, and a slime block lifts
// what stands on it.
// vanilla: PistonMovingBlockEntity.moveCollidedEntities

// TestAPistonHeadLeavesAPlayerInItsCellToTheClient extends a piston into the
// cell a player stands in. The client moves itself, so the server keeps the
// position the client reported.
func TestAPistonHeadLeavesAPlayerInItsCellToTheClient(t *testing.T) {
	t.Parallel()
	srv, actor := servertest.Join(t, servertest.BlockTestConfig(), "Actor", servertest.OpsListing(t, "Actor"))

	servertest.Setblock(t, actor, 2, 64, 0, "piston[facing=east]")
	actor.MoveTo(3.5, 64, 0.5, 90, 0)
	actor.Settle()
	servertest.Setblock(t, actor, 2, 64, 1, "redstone_block")
	servertest.AwaitTicks(t, srv, 6)
	actor.Settle()

	got := srv.PlayerPosition("Actor")
	if got.X != 3.5 || got.Y != 64 || got.Z != 0.5 {
		t.Errorf("the player stood at %v after the extension, want the reported 3.5, 64, 0.5", got)
	}
}

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

// TestAPushedItemKeepsItsSideVelocity lifts a moving item on a slime block.
// The bounce sets only the vertical velocity.
func TestAPushedItemKeepsItsSideVelocity(t *testing.T) {
	t.Parallel()
	srv, actor := servertest.Join(t, servertest.BlockTestConfig(), "Actor", servertest.OpsListing(t, "Actor"))

	servertest.Setblock(t, actor, 2, 64, 0, "piston[facing=up]")
	servertest.Setblock(t, actor, 2, 65, 0, "slime_block")
	actor.Settle()
	// The power goes in from the console straight after the spawn: sent
	// over the connection, it could land ticks later, by which time the
	// ground's friction has taken the slide off before the lift.
	srv.SpawnItemEntityMotion(2.5, 66.13, 0.5, "minecraft:stone", 1, 0.3, 0, 0)
	srv.RunLine("setblock 3 64 0 minecraft:redstone_block")
	servertest.AwaitTicks(t, srv, 3)

	items := srv.ItemsInDimension("minecraft:overworld")
	if len(items) != 1 {
		t.Fatalf("%d items, want the one stack", len(items))
	}
	if items[0].VX < 0.05 {
		t.Errorf("the item's x velocity is %v after the lift, want the slide kept", items[0].VX)
	}
}

// TestAHoneyBlockCarriesARestingOrb slides a honey block sideways under an
// orb that has settled on it.
func TestAHoneyBlockCarriesARestingOrb(t *testing.T) {
	t.Parallel()
	srv, actor := servertest.Join(t, servertest.BlockTestConfig(), "Actor", servertest.OpsListing(t, "Actor"))

	servertest.Setblock(t, actor, 20, 80, 0, "piston[facing=east]")
	servertest.Setblock(t, actor, 21, 80, 0, "honey_block")
	actor.Settle()
	id := srv.SpawnOrb(21.5, 81.2, 0.5, 1)
	servertest.AwaitTicks(t, srv, 10)
	servertest.Setblock(t, actor, 20, 81, 0, "redstone_block")
	servertest.AwaitTicks(t, srv, 6)

	x, _, _, ok := srv.OrbPos(id)
	if !ok {
		t.Fatal("the orb is gone")
	}
	if x < 22.45 {
		t.Errorf("the orb lay at x %v after the honey block slid, want carried to x >= 22.45", x)
	}
}
