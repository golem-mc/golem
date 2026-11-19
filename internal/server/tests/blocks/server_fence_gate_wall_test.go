package blocks

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/registry"
)

func gateInWall(srv interface{ BlockAt(x, y, z int32) uint16 }, x, y, z int32) string {
	_, props := servertest.StateProps(srv.BlockAt(x, y, z))
	return props["in_wall"]
}

// TestFenceGatePlacedBetweenWallsIsInWall places a gate between two walls
// across its facing, and one with no wall on that axis.
// vanilla: FenceGateBlock.getStateForPlacement
func TestFenceGatePlacedBetweenWallsIsInWall(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Builder", servertest.OpsListing(t, "Builder"))

	c.ChatCommand("setblock 4 64 5 cobblestone_wall")
	c.ChatCommand("setblock 6 64 5 cobblestone_wall")
	c.ChatCommand("setblock 5 64 8 cobblestone_wall")
	servertest.WaitForBlock(t, srv, 6, 64, 5, func(s uint16) bool { return s != 0 })
	servertest.WaitForBlock(t, srv, 5, 64, 8, func(s uint16) bool { return s != 0 })

	gate, _ := registry.ItemID("minecraft:oak_fence_gate")
	c.SetCreativeSlot(player.HotbarSlot, protocol.ItemStack{ItemID: gate, Count: 1})
	c.MoveToGround(5.5, 64, 2.5, true)
	c.Settle()

	// Facing south, the walls on the x axis lower the gate.
	c.FacePlayer(5.5, 64, 2.5, 0, 0)
	c.PlaceBlock(5, 63, 5, protocol.FaceTop)
	servertest.WaitForTicks(t, srv, 60, func() bool { return gateInWall(srv, 5, 64, 5) == "true" })

	// The wall in front of the gate is on its own axis: not in a wall.
	c.PlaceBlock(5, 63, 7, protocol.FaceTop)
	servertest.WaitForTicks(t, srv, 60, func() bool { return gateInWall(srv, 5, 64, 7) == "false" })
}

// TestFenceGateFollowsWallBeside builds a wall next to a standing gate and
// breaks it again, by command and by hand.
// vanilla: FenceGateBlock.updateShape
func TestFenceGateFollowsWallBeside(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Builder", servertest.OpsListing(t, "Builder"))

	c.ChatCommand("setblock 5 64 5 oak_fence_gate[facing=south]")
	servertest.WaitForBlock(t, srv, 5, 64, 5, func(s uint16) bool { return s != 0 })
	if got := gateInWall(srv, 5, 64, 5); got != "false" {
		t.Fatalf("lone gate in_wall = %q, want false", got)
	}

	c.ChatCommand("setblock 6 64 5 cobblestone_wall")
	servertest.WaitForTicks(t, srv, 60, func() bool { return gateInWall(srv, 5, 64, 5) == "true" })

	// A wall on the far side keeps it lowered after the first goes.
	c.ChatCommand("setblock 4 64 5 cobblestone_wall")
	c.ChatCommand("setblock 6 64 5 air")
	servertest.WaitForBlock(t, srv, 6, 64, 5, func(s uint16) bool { return s == 0 })
	servertest.RunTicks(t, srv, 4)
	if got := gateInWall(srv, 5, 64, 5); got != "true" {
		t.Fatalf("gate with one wall left in_wall = %q, want true", got)
	}

	c.MoveToGround(4.5, 64, 2.5, true)
	c.Settle()
	c.BreakBlock(4, 64, 5)
	servertest.WaitForTicks(t, srv, 60, func() bool { return gateInWall(srv, 5, 64, 5) == "false" })

	// A wall north of a south-facing gate is on its own axis.
	c.ChatCommand("setblock 5 64 4 cobblestone_wall")
	servertest.WaitForBlock(t, srv, 5, 64, 4, func(s uint16) bool { return s != 0 })
	servertest.RunTicks(t, srv, 4)
	if got := gateInWall(srv, 5, 64, 5); got != "false" {
		t.Fatalf("gate with a wall in front in_wall = %q, want false", got)
	}
}
