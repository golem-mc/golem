package redstone

import (
	"testing"

	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// pistonPushRig joins an operator with the loop held, stone through the destination at Y=63
// and a piston at (2, 64, 3) facing east, the cell in front of it clear.
func pistonPushRig(t *testing.T) (*server.Server, *servertest.Client) {
	t.Helper()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Pusher", servertest.OpsListing(t, "Pusher"))
	servertest.HoldTicks(t, srv)
	for x := int32(3); x <= 9; x++ {
		srv.SetBlock(x, 63, 3, registry.BlockStone.DefaultState())
	}
	servertest.Setblock(t, c, 2, 64, 3, "piston[facing=east]")
	return srv, c
}

// TestAPistonPushesAMinecart extends a piston into a cart standing in front
// of it: the cart moves one block along the push without turning.
// vanilla: PistonMovingBlockEntity.moveCollidedEntities
func TestAPistonPushesAMinecart(t *testing.T) {
	t.Parallel()
	srv, c := pistonPushRig(t)
	id := srv.SpawnMinecart(3.5, 64, 3.5)
	srv.RunLine("data merge entity @e[type=minecraft:minecart,limit=1] {Rotation:[90f,0f]}")
	if _, _, _, yaw, ok := srv.MinecartState(id); !ok || yaw != 90 {
		t.Fatalf("initial cart yaw=%v (alive %v), want 90", yaw, ok)
	}
	servertest.Setblock(t, c, 2, 65, 3, "redstone_block")
	servertest.RunTicks(t, srv, 3)
	x, _, _, yaw, ok := srv.MinecartState(id)
	if !ok || x != 4.5 {
		t.Errorf("the cart stands at x=%v (alive %v), want 4.5", x, ok)
	}
	if yaw != 90 {
		t.Errorf("the cart's yaw=%v after the push, want 90", yaw)
	}
}
