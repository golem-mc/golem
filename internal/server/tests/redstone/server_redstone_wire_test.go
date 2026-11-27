package redstone

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
)

// The wire's connections around a placement, a break and a /setblock.
// vanilla: RedStoneWireBlock.getConnectingSide/updateShape/
// updateIndirectNeighbourShapes, BlockBehaviour.updateNeighbourShapes.

// TestPlacingWireConnectsTheWireAlreadyThere places a dust beside an existing
// one; both must face each other.
func TestPlacingWireConnectsTheWireAlreadyThere(t *testing.T) {
	t.Parallel()
	srv, c := servertest.AttachActor(t)

	servertest.Setblock(t, c, 5, 64, 5, "redstone_wire")
	c.Settle()
	servertest.PlaceFrom(c, "minecraft:redstone", 6, 4, 0, 6, 63, 5, protocol.FaceTop)
	c.Settle()

	servertest.ExpectBlockNamed(t, srv, 5, 64, 5, "minecraft:redstone_wire", map[string]string{"east": "side"})
	servertest.ExpectBlockNamed(t, srv, 6, 64, 5, "minecraft:redstone_wire", map[string]string{"west": "side"})
}

// TestPlacingCrestWireConnectsTheDustAtTheFoot places a wire on top of a step
// after the dot at its foot; the foot climbs the step.
func TestPlacingCrestWireConnectsTheDustAtTheFoot(t *testing.T) {
	t.Parallel()
	srv, c := servertest.AttachActor(t)

	servertest.Setblock(t, c, 6, 64, 5, "stone")
	servertest.Setblock(t, c, 5, 64, 5, "redstone_wire")
	c.Settle()
	servertest.PlaceFrom(c, "minecraft:redstone", 6, 6, 0, 6, 64, 5, protocol.FaceTop)
	c.Settle()

	servertest.ExpectBlockNamed(t, srv, 5, 64, 5, "minecraft:redstone_wire", map[string]string{"east": "up"})
	servertest.ExpectBlockNamed(t, srv, 6, 65, 5, "minecraft:redstone_wire", map[string]string{"west": "side"})
}

// TestBreakingTheStepRemovesTheClimb breaks the step a foot dust climbs: the
// climb goes and the dust on the step falls with its support.
func TestBreakingTheStepRemovesTheClimb(t *testing.T) {
	t.Parallel()
	srv, c := servertest.AttachActor(t)

	servertest.Setblock(t, c, 6, 64, 5, "stone")
	servertest.Setblock(t, c, 6, 65, 5, "redstone_wire")
	servertest.Setblock(t, c, 4, 64, 5, "redstone_wire")
	servertest.Setblock(t, c, 5, 64, 5, "redstone_wire[east=up]")
	c.Settle()

	servertest.Setblock(t, c, 6, 64, 5, "air")
	c.Settle()

	servertest.ExpectBlockNamed(t, srv, 5, 64, 5, "minecraft:redstone_wire", map[string]string{
		"east": "side", "north": "none", "south": "none", "west": "side",
	})
	servertest.ExpectBlockNamed(t, srv, 6, 65, 5, "minecraft:air", nil)
}

// TestClickingALoneDustTogglesItToACross right-clicks an isolated dust dot:
// the wire's own use flips a dot to the cross state.
// vanilla: RedStoneWireBlock.useWithoutItem
func TestClickingALoneDustTogglesItToACross(t *testing.T) {
	t.Parallel()
	srv, c := servertest.AttachActor(t)

	servertest.Setblock(t, c, 2, 64, 0, "redstone_wire")
	c.Settle()
	servertest.ExpectBlockNamed(t, srv, 2, 64, 0, "minecraft:redstone_wire",
		map[string]string{"north": "none", "south": "none", "east": "none", "west": "none"})

	c.UseBlock(2, 64, 0)
	c.Settle()
	servertest.ExpectBlockNamed(t, srv, 2, 64, 0, "minecraft:redstone_wire",
		map[string]string{"north": "side", "south": "side", "east": "side", "west": "side"})
}

// TestClickingAConnectedCrossLeavesTheWireAlone is the toggle that is a
// no-op: the four sides a cross is connected through are real neighbour
// wires, so re-deriving them makes the same cross and nothing is written.
// A no-op is a PASS, so a held block's own placement still goes ahead.
// vanilla: RedStoneWireBlock.useWithoutItem
func TestClickingAConnectedCrossLeavesTheWireAlone(t *testing.T) {
	t.Parallel()
	srv, c := servertest.AttachActor(t)

	servertest.Setblock(t, c, 1, 64, 0, "redstone_wire")
	servertest.Setblock(t, c, 3, 64, 0, "redstone_wire")
	servertest.Setblock(t, c, 2, 64, -1, "redstone_wire")
	servertest.Setblock(t, c, 2, 64, 1, "redstone_wire")
	servertest.Setblock(t, c, 2, 64, 0, "redstone_wire")
	c.Settle()
	servertest.ExpectBlockNamed(t, srv, 2, 64, 0, "minecraft:redstone_wire",
		map[string]string{"north": "side", "south": "side", "east": "side", "west": "side"})

	c.UseBlock(2, 64, 0)
	c.Settle()
	servertest.ExpectBlockNamed(t, srv, 2, 64, 0, "minecraft:redstone_wire",
		map[string]string{"north": "side", "south": "side", "east": "side", "west": "side"})

	c.SetCreativeSlot(player.HotbarSlot, servertest.ItemStack(t, "minecraft:stone", 1))
	c.PlaceBlock(2, 64, 0, protocol.FaceTop)
	c.Settle()
	servertest.ExpectBlockNamed(t, srv, 2, 65, 0, "minecraft:stone", nil)
}

// TestASlopedLinePowersAndSheds feeds a wire at the foot of a step from a
// redstone block, through the step onto a crest wire and a lamp, then
// removes the source: every wire in the slope must come back to zero.
func TestASlopedLinePowersAndSheds(t *testing.T) {
	t.Parallel()
	srv, c := servertest.AttachActor(t)

	servertest.Setblock(t, c, 6, 64, 5, "stone")
	servertest.Setblock(t, c, 7, 65, 5, "redstone_lamp")
	servertest.Setblock(t, c, 6, 65, 5, "redstone_wire[west=side,east=side]")
	servertest.Setblock(t, c, 5, 64, 5, "redstone_wire[east=up]")
	servertest.Setblock(t, c, 4, 64, 5, "redstone_block")
	c.Settle()

	servertest.ExpectBlockNamed(t, srv, 6, 65, 5, "minecraft:redstone_wire", map[string]string{"power": "14"})
	servertest.ExpectBlockNamed(t, srv, 7, 65, 5, "minecraft:redstone_lamp", map[string]string{"lit": "true"})

	servertest.Setblock(t, c, 4, 64, 5, "air")
	c.PumpTicks(srv, 8, func(int32, []byte) bool { return false })

	servertest.ExpectBlockNamed(t, srv, 5, 64, 5, "minecraft:redstone_wire", map[string]string{"power": "0"})
	servertest.ExpectBlockNamed(t, srv, 6, 65, 5, "minecraft:redstone_wire", map[string]string{"power": "0"})
	servertest.ExpectBlockNamed(t, srv, 7, 65, 5, "minecraft:redstone_lamp", map[string]string{"lit": "false"})
}

func wireSides(t *testing.T, srv interface{ BlockAt(x, y, z int32) uint16 }, x, y, z int32) string {
	t.Helper()
	name, props := servertest.StateProps(srv.BlockAt(x, y, z))
	if name != "minecraft:redstone_wire" {
		t.Fatalf("block at (%d,%d,%d) is %q, want a wire", x, y, z, name)
	}
	short := map[string]string{"none": "n", "side": "s", "up": "u"}
	return short[props["north"]] + short[props["east"]] + short[props["south"]] + short[props["west"]]
}

// TestSetblockWireSidesFollowTheNeighboursAndTheGivenProperties pins that a
// command-placed wire takes the sides the neighbours ask for, and keeps the
// ones the block argument spelled out beyond that.
// vanilla: RedStoneWireBlock.getConnectionState
func TestSetblockWireSidesFollowTheNeighboursAndTheGivenProperties(t *testing.T) {
	t.Parallel()
	srv, c := servertest.AttachActor(t)
	for i, tc := range []struct{ block, want string }{
		{"redstone_wire[east=side]", "ssss"},
		{"redstone_wire[north=side]", "ssss"},
		{"redstone_wire[north=side,south=side]", "ssss"},
		{"redstone_wire[east=up]", "suss"},
	} {
		x := int32(10 + 2*i)
		servertest.Setblock(t, c, x, 64, 5, tc.block)
		c.Settle()
		if got := wireSides(t, srv, x, 64, 5); got != tc.want {
			t.Errorf("%s placed as %s, want %s", tc.block, got, tc.want)
		}
	}
}

// TestDecayingWireWakeOrderPulsesSideBranch pins that a decaying wire wakes its
// cell and six neighbours in vanilla's 16-bucket HashSet<BlockPos> order: when
// replacing a redstone block with unpowered dust, the west wire's SOUTH
// neighbour (under an unpowered step-up branch) runs before its EAST neighbour
// at coordinates where SOUTH hashes into the same or earlier bucket, transiently
// powering the branch and scheduling a repeater pulse two ticks later.
// vanilla: DefaultRedstoneWireEvaluator.updatePowerStrength, Vec3i.hashCode
func TestDecayingWireWakeOrderPulsesSideBranch(t *testing.T) {
	t.Parallel()
	srv, c := servertest.AttachActor(t)

	// At (4, 70, 4), the decaying line's HashSet<BlockPos> order visits the
	// west wire's SOUTH neighbour (under the step-up branch) while (4, 70, 4)
	// still holds transient power 14, whereas a fixed [SELF, W, E, D, U, N, S]
	// order decays the line to 0 first.
	const bx, by, bz int32 = 4, 70, 4
	servertest.Setblock(t, c, bx-1, by-1, bz-1, "stone")
	servertest.Setblock(t, c, bx, by-1, bz-1, "stone")
	servertest.Setblock(t, c, bx+1, by-1, bz-1, "stone")
	servertest.Setblock(t, c, bx, by-1, bz, "stone")
	servertest.Setblock(t, c, bx-1, by, bz, "stone")
	servertest.Setblock(t, c, bx-1, by, bz+1, "stone")

	// Powered wire line fed by the redstone block at (bx, by, bz).
	servertest.Setblock(t, c, bx, by, bz, "redstone_block")
	servertest.Setblock(t, c, bx-1, by, bz-1, "redstone_wire")
	servertest.Setblock(t, c, bx, by, bz-1, "redstone_wire")
	servertest.Setblock(t, c, bx+1, by, bz-1, "redstone_wire")

	// Side branch one block up, disconnected from the redstone block, feeding a
	// 1-tick repeater directly at (bx-1, by+1, bz+1).
	servertest.Setblock(t, c, bx-1, by+1, bz, "redstone_wire")
	servertest.Setblock(t, c, bx-1, by+1, bz+1, "repeater[delay=1,facing=north,locked=false,powered=false]")
	c.Settle()

	servertest.ExpectBlockNamed(t, srv, bx-1, by+1, bz+1, "minecraft:repeater", map[string]string{"powered": "false"})

	servertest.HoldTicks(t, srv)
	// Replace the redstone block with unpowered dust; its onPlace briefly reads
	// power 14 from (bx, by, bz-1) before the line decays to 0.
	c.ChatCommand("setblock 4 70 4 redstone_wire[east=none,north=side,power=0,south=none,west=up]")
	servertest.RunTicks(t, srv, 1)
	servertest.AwaitCommandFeedback(t, c, "commands.setblock.success", "4", "70", "4")

	servertest.ExpectBlockNamed(t, srv, bx-1, by+1, bz+1, "minecraft:repeater", map[string]string{"powered": "false"})
	servertest.RunTicks(t, srv, 2)
	servertest.ExpectBlockNamed(t, srv, bx-1, by+1, bz+1, "minecraft:repeater", map[string]string{"powered": "true"})
	servertest.RunTicks(t, srv, 1)
	servertest.ExpectBlockNamed(t, srv, bx-1, by+1, bz+1, "minecraft:repeater", map[string]string{"powered": "true"})
	servertest.RunTicks(t, srv, 1)
	servertest.ExpectBlockNamed(t, srv, bx-1, by+1, bz+1, "minecraft:repeater", map[string]string{"powered": "false"})
}
