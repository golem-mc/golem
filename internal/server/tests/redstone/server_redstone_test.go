package redstone

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/registry"
)

// The strong power a lit dust line gives the block it lies on: the dust
// strongly powers the block below it, and that block passes the power to
// every neighbour. vanilla: RedStoneWireBlock.getDirectSignal,
// SignalGetter.getSignal/getDirectSignalTo, TntBlock.neighborChanged.

// TestLitDustPrimesTNTBesideTheBlockBelow checks the positive: a straight lit
// line of dust on a row of stone, and TNT beside that stone one level below
// the dust, primes because the stone is strongly powered.
func TestLitDustPrimesTNTBesideTheBlockBelow(t *testing.T) {
	t.Parallel()
	srv, c := servertest.AttachActor(t)

	// The stone row under the line and the redstone block powering its head.
	servertest.Setblock(t, c, 5, 64, 5, "stone")
	servertest.Setblock(t, c, 6, 64, 5, "stone")
	servertest.Setblock(t, c, 7, 64, 5, "stone")
	servertest.Setblock(t, c, 4, 65, 5, "redstone_block")
	servertest.Setblock(t, c, 5, 65, 5, "redstone_wire[east=side]")
	servertest.Setblock(t, c, 6, 65, 5, "redstone_wire[east=side,west=side]")
	servertest.Setblock(t, c, 7, 65, 5, "redstone_wire[west=side]")

	// TNT beside the stone under the line's middle cell, one level below the
	// dust: the strongly powered stone primes it. The loop stops first so
	// the body is read where the prime put it, not a tick into its fall.
	srv.StopTick()
	servertest.Setblock(t, c, 6, 64, 6, "tnt")
	c.Settle()

	if got := srv.BlockAt(6, 64, 6); got == registry.BlockStates["minecraft:tnt"] {
		t.Fatal("the TNT beside the strongly powered stone never primed")
	}
	var found bool
	for _, it := range srv.TNTsInDimension("minecraft:overworld") {
		if it.X == 6.5 && it.Y == 64 && it.Z == 6.5 {
			found = true
		}
	}
	if !found {
		t.Errorf("no primed TNT stood beside the stone: %v", srv.TNTsInDimension("minecraft:overworld"))
	}
}

// TestLeverThroughTheFloorLightsTheLampBelow pulls a lever on a stone floor
// with a lamp under the floor: the lever strongly powers the stone and the
// lamp, whose only neighbour is the stone, reads that strong power through
// the conductor and lights. vanilla: SignalGetter.getSignal/getDirectSignalTo.
func TestLeverThroughTheFloorLightsTheLampBelow(t *testing.T) {
	t.Parallel()
	srv, c := servertest.AttachActor(t)

	servertest.Setblock(t, c, 3, 64, 0, "stone")
	servertest.Setblock(t, c, 3, 63, 0, "redstone_lamp")
	servertest.Setblock(t, c, 3, 65, 0, "lever[face=floor,facing=north]")
	c.Settle()

	c.UseBlock(3, 65, 0)
	c.Settle()
	servertest.ExpectBlockNamed(t, srv, 3, 63, 0, "minecraft:redstone_lamp", map[string]string{"lit": "true"})
}

// TestPlayerPlacedLoneDustIsACross places a single dust with nothing that
// connects to it: the placement's getConnectionState runs the isolated-pair
// rule over an all-empty neighbourhood and gives every side a side, so the
// dust is the four-armed cross, not an east-west line.
// vanilla: RedStoneWireBlock.getStateForPlacement/getConnectionState.
func TestPlayerPlacedLoneDustIsACross(t *testing.T) {
	t.Parallel()
	srv, c := servertest.AttachActor(t)

	servertest.PlaceFrom(c, "minecraft:redstone", 4, 5, 0, 4, 63, 6, protocol.FaceTop)

	servertest.ExpectBlockNamed(t, srv, 4, 64, 6, "minecraft:redstone_wire", map[string]string{
		"east": "side", "north": "side", "south": "side", "west": "side",
	})
}

// TestRemovingASourceDropsDustFedThroughABlock covers a lever on a block
// strongly powering it, and dust on the ground beside that block reading the
// power through it — a straight run, a branch and a loop. Removing the lever
// has to take every wire to 0, including the head wire that only ever read
// the block. vanilla: RedStoneWireBlock.getBlockSignal, Level.getSignal.
func TestRemovingASourceDropsDustFedThroughABlock(t *testing.T) {
	t.Parallel()
	srv, c := servertest.AttachActor(t)
	servertest.HoldTicks(t, srv)

	servertest.Setblock(t, c, 4, 64, 5, "stone")
	servertest.Setblock(t, c, 4, 65, 5, "lever[face=floor,facing=north,powered=true]")
	servertest.Setblock(t, c, 5, 64, 5, "redstone_wire")
	servertest.Setblock(t, c, 6, 64, 5, "redstone_wire")
	servertest.Setblock(t, c, 7, 64, 5, "redstone_wire")
	servertest.Setblock(t, c, 6, 64, 4, "redstone_wire")
	servertest.Setblock(t, c, 8, 64, 5, "redstone_wire")
	servertest.Setblock(t, c, 8, 64, 6, "redstone_wire")
	servertest.Setblock(t, c, 7, 64, 6, "redstone_wire")
	c.Settle()

	cells := [][3]int32{
		{5, 64, 5}, {6, 64, 5}, {7, 64, 5}, {6, 64, 4},
		{8, 64, 5}, {8, 64, 6}, {7, 64, 6},
	}
	for _, p := range cells {
		if _, props := servertest.StateProps(srv.BlockAt(p[0], p[1], p[2])); props["power"] == "0" {
			t.Fatalf("setup: dust at %v never lit", p)
		}
	}

	servertest.Setblock(t, c, 4, 65, 5, "air")
	c.PumpTicks(srv, 25, func(int32, []byte) bool { return false })
	for _, p := range cells {
		name, props := servertest.StateProps(srv.BlockAt(p[0], p[1], p[2]))
		if name == "minecraft:redstone_wire" && props["power"] != "0" {
			t.Errorf("dust at %v has power %s after the lever went, want 0", p, props["power"])
		}
	}
}

// TestBreakingAPressedButtonReleasesTheBlockItHangsOn presses a wall and a
// ceiling button and breaks each while it is down. The dust beside the block
// a button hangs on has to go dark, which needs the removal to wake that
// block's neighbours rather than the block under the button.
func TestBreakingAPressedButtonReleasesTheBlockItHangsOn(t *testing.T) {
	t.Parallel()
	t.Run("wall", func(t *testing.T) {
		t.Parallel()
		srv, c := servertest.AttachActor(t)

		servertest.Setblock(t, c, 5, 64, 0, "stone")
		servertest.Setblock(t, c, 6, 64, 0, "redstone_wire")
		servertest.Setblock(t, c, 4, 64, 0, "oak_button[face=wall,facing=west,powered=false]")
		c.Settle()

		c.UseBlock(4, 64, 0)
		c.Settle()
		servertest.ExpectBlockNamed(t, srv, 6, 64, 0, "minecraft:redstone_wire", map[string]string{"power": "15"})

		servertest.Setblock(t, c, 4, 64, 0, "air")
		c.Settle()
		servertest.ExpectBlockNamed(t, srv, 6, 64, 0, "minecraft:redstone_wire", map[string]string{"power": "0"})
	})
	t.Run("ceiling", func(t *testing.T) {
		t.Parallel()
		srv, c := servertest.AttachActor(t)

		servertest.Setblock(t, c, 6, 64, 0, "stone")
		servertest.Setblock(t, c, 6, 65, 0, "redstone_wire")
		servertest.Setblock(t, c, 5, 65, 0, "stone")
		servertest.Setblock(t, c, 5, 64, 0, "oak_button[face=ceiling,facing=north,powered=false]")
		c.Settle()

		c.UseBlock(5, 64, 0)
		c.Settle()
		servertest.ExpectBlockNamed(t, srv, 6, 65, 0, "minecraft:redstone_wire", map[string]string{"power": "15"})

		servertest.Setblock(t, c, 5, 64, 0, "air")
		c.Settle()
		servertest.ExpectBlockNamed(t, srv, 6, 65, 0, "minecraft:redstone_wire", map[string]string{"power": "0"})
	})
}

// TestPoweringATorchsBaseTurnsOffTheLampAbove lights a lamp on a block a
// torch stands under, then powers the torch's base with a wall lever: the
// torch goes dark two ticks later and the lamp must go with it, which needs
// the torch's state change to wake the neighbours of its neighbours.
// vanilla: RedstoneTorchBlock.tick/notifyNeighbors.
func TestPoweringATorchsBaseTurnsOffTheLampAbove(t *testing.T) {
	t.Parallel()
	srv, c := servertest.AttachActor(t)

	servertest.Setblock(t, c, 3, 64, 0, "stone")
	servertest.Setblock(t, c, 3, 65, 0, "redstone_torch")
	servertest.Setblock(t, c, 3, 66, 0, "stone")
	servertest.Setblock(t, c, 3, 67, 0, "redstone_lamp")
	servertest.Setblock(t, c, 3, 68, 0, "stone")
	servertest.Setblock(t, c, 2, 64, 0, "lever[face=wall,facing=west]")
	c.Settle()
	servertest.ExpectBlockNamed(t, srv, 3, 67, 0, "minecraft:redstone_lamp", map[string]string{"lit": "true"})

	lampOff, _ := registry.FindState("minecraft:redstone_lamp", map[string]string{"lit": "false"})
	c.UseBlock(2, 64, 0)
	c.Settle()
	servertest.WaitForTicks(t, srv, 20, func() bool { return srv.BlockAt(3, 67, 0) == lampOff })

	servertest.ExpectBlockNamed(t, srv, 3, 65, 0, "minecraft:redstone_torch", map[string]string{"lit": "false"})
	servertest.ExpectBlockNamed(t, srv, 3, 67, 0, "minecraft:redstone_lamp", map[string]string{"lit": "false"})
}

// TestRepeaterPlacedBesideAPoweredDiodeArrivesLocked places a repeater beside
// a powered diode aimed at its side. The placement reads the lock and tells
// the clients the LOCKED state; unpowering the side writes it back off.
// vanilla: RepeaterBlock.getStateForPlacement, updateShape.
func TestRepeaterPlacedBesideAPoweredDiodeArrivesLocked(t *testing.T) {
	t.Parallel()
	srv, c := servertest.AttachActor(t)
	srv.StopTick()

	// A powered repeater at (5, 64, 5) faces south: its output points north,
	// into the side of the repeater placed at (5, 64, 4), on the stone there.
	servertest.Setblock(t, c, 5, 64, 5, "repeater[facing=south,powered=true]")

	locked, _ := registry.FindState("minecraft:repeater", map[string]string{"facing": "west", "locked": "true"})
	unlocked, _ := registry.FindState("minecraft:repeater", map[string]string{"facing": "west"})

	// The actor stands north of the support and looks east (yaw 270), so the
	// repeater it sets down faces west and its axis runs across the diode.
	id, _ := registry.ItemID("minecraft:repeater")
	c.SetCreativeSlot(player.HotbarSlot, protocol.ItemStack{ItemID: id, Count: 1})
	c.MoveToGround(5.5, 64, 3.5, true)
	c.FacePlayer(5.5, 64, 3.5, 270, 0)
	c.Settle()
	c.PlaceBlock(5, 63, 4, protocol.FaceTop)
	servertest.AwaitBlockUpdate(t, c, 5, 64, 4, locked)
	servertest.ExpectBlockNamed(t, srv, 5, 64, 4, "minecraft:repeater", map[string]string{"facing": "west", "locked": "true"})

	// The placement's tail echoes the state a second time; drain it so the
	// update the unlock writes is the one the next read finds.
	c.Settle()

	// Take the side diode's power away: the shape update writes LOCKED off.
	servertest.Setblock(t, c, 5, 64, 5, "repeater[facing=south,powered=false]")
	servertest.AwaitBlockUpdate(t, c, 5, 64, 4, unlocked)
	servertest.ExpectBlockNamed(t, srv, 5, 64, 4, "minecraft:repeater", map[string]string{"facing": "west", "locked": "false"})
}

// pistonFacingDelta is the cell step each facing property names.
func pistonFacingDelta(facing string) (int32, int32, int32) {
	switch facing {
	case "down":
		return 0, -1, 0
	case "up":
		return 0, 1, 0
	case "north":
		return 0, 0, -1
	case "south":
		return 0, 0, 1
	case "west":
		return -1, 0, 0
	default:
		return 1, 0, 0
	}
}

// TestPistonIgnoresARedstoneBlockInFrontOnTheOtherFacings is the same rule for
// the five facings but down: the redstone block in front powers nothing, so
// the piston holds its ground and the block does not move.
// vanilla: PistonBaseBlock.getNeighborSignal.
func TestPistonIgnoresARedstoneBlockInFrontOnTheOtherFacings(t *testing.T) {
	t.Parallel()
	for i, facing := range []string{"up", "north", "south", "west", "east"} {
		t.Run(facing, func(t *testing.T) {
			t.Parallel()
			srv, c := servertest.AttachActor(t)

			dx, dy, dz := pistonFacingDelta(facing)
			for j, tc := range []struct{ name, piston string }{
				{"plain", "piston"},
				{"sticky", "sticky_piston"},
			} {
				bx, by, bz := int32(8+i), int32(70), int32(8+j*4)
				fx, fy, fz := bx+dx, by+dy, bz+dz
				servertest.Setblock(t, c, bx, by, bz, tc.piston+"[facing="+facing+"]")
				c.Settle()
				servertest.ExpectBlockNamed(t, srv, bx, by, bz, "minecraft:"+tc.piston, map[string]string{"extended": "false"})

				// The redstone block in front is the wake; the cells beyond
				// it are air, so a buggy extend has somewhere to push it.
				servertest.Setblock(t, c, fx, fy, fz, "redstone_block")
				c.Settle()
				c.PumpTicks(srv, 40, func(int32, []byte) bool { return false })

				servertest.ExpectBlockNamed(t, srv, bx, by, bz, "minecraft:"+tc.piston, map[string]string{"extended": "false", "facing": facing})
				servertest.ExpectBlockNamed(t, srv, fx, fy, fz, "minecraft:redstone_block", nil)
			}
		})
	}
}

// TestPistonReadsQuasiConnectivityFromTwoAbove is the piston's quasi reading:
// a redstone block two cells above a retracted piston powers it once a block
// update wakes the piston. vanilla: PistonBaseBlock.getNeighborSignal.
func TestPistonReadsQuasiConnectivityFromTwoAbove(t *testing.T) {
	t.Parallel()
	srv, c := servertest.AttachActor(t)

	servertest.Setblock(t, c, 5, 64, 5, "piston[facing=east]")
	servertest.Setblock(t, c, 5, 66, 5, "redstone_block")
	c.Settle()

	// The power is not beside the piston, so nothing has woken it.
	servertest.ExpectBlockNamed(t, srv, 5, 64, 5, "minecraft:piston", map[string]string{"extended": "false"})

	// A stone beside the piston wakes it and the quasi reading runs.
	servertest.Setblock(t, c, 4, 64, 5, "stone")
	c.PumpTicks(srv, 8, func(int32, []byte) bool { return false })

	servertest.ExpectBlockNamed(t, srv, 5, 64, 5, "minecraft:piston", map[string]string{"extended": "true"})
}
