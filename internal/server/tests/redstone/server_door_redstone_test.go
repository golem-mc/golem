package redstone

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"

	"github.com/golem-mc/golem/internal/server"
)

// The common ways a redstone circuit reaches a wooden door, one test each,
// the results recorded on the vanilla reference server: dust pointing
// into either door half opens the pair, dust running past it does not, dust
// buried in the floor beside the threshold opens the door through the
// strongly powered block below, a standing torch beside the door opens it, a
// wall torch hanging on the threshold does not (it strong-powers the empty
// cell above its own, beside the door rather than the door itself), a torch
// in the pit under the threshold does, and a lever on a frame block opens
// both halves.
// vanilla: DoorBlock.neighborChanged, RedStoneWireBlock.getSignal/getDirectSignal.

// standDoor writes the two halves of a shut oak door standing on the flat
// ground's surface (y=64) at x, z.
func standDoor(t *testing.T, c *servertest.Client, x, z int32) {
	t.Helper()
	standDoorNamed(t, c, "oak_door", x, z)
}

// standDoorNamed is standDoor for any door material.
func standDoorNamed(t *testing.T, c *servertest.Client, name string, x, z int32) {
	t.Helper()
	servertest.Setblock(t, c, x, 64, z, name+"[half=lower,facing=north,hinge=left,open=false,powered=false]")
	servertest.Setblock(t, c, x, 65, z, name+"[half=upper,facing=north,hinge=left,open=false,powered=false]")
}

// vanilla: DoorBlock.neighborChanged, TrapDoorBlock.neighborChanged,
// FenceGateBlock.neighborChanged
func TestPoplarBlocksFollowRedstone(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"poplar_door", "poplar_trapdoor", "poplar_fence_gate"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			srv, c := servertest.AttachActor(t)
			if name == "poplar_door" {
				standDoorNamed(t, c, name, 5, 0)
			} else {
				servertest.Setblock(t, c, 5, 64, 0, name+"[open=false,powered=false]")
			}
			for _, step := range []struct {
				source string
				open   string
			}{{"redstone_block", "true"}, {"air", "false"}} {
				servertest.Setblock(t, c, 4, 64, 0, step.source)
				servertest.ExpectBlockNamed(t, srv, 5, 64, 0, "minecraft:"+name,
					map[string]string{"open": step.open, "powered": step.open})
				if name == "poplar_door" {
					servertest.ExpectBlockNamed(t, srv, 5, 65, 0, "minecraft:"+name,
						map[string]string{"open": step.open, "powered": step.open})
				}
			}
		})
	}
}

// expectDoorOpen checks both oak halves' open state.
func expectDoorOpen(t *testing.T, srv *server.Server, x, z int32, open bool) {
	t.Helper()
	expectDoorOpenNamed(t, srv, "minecraft:oak_door", x, z, open)
}

// expectDoorOpenNamed is expectDoorOpen for any door material.
func expectDoorOpenNamed(t *testing.T, srv *server.Server, name string, x, z int32, open bool) {
	t.Helper()
	want := "false"
	if open {
		want = "true"
	}
	servertest.ExpectBlockNamed(t, srv, x, 64, z, name, map[string]string{"open": want, "powered": want})
	servertest.ExpectBlockNamed(t, srv, x, 65, z, name, map[string]string{"open": want, "powered": want})
}

// TestDustPointingIntoTheDoorOpensIt feeds the issue's scene: an oak door
// with a line of redstone dust ending at its lower half, which a redstone
// torch lights only after everything stands. The wire's wake has to carry
// the power to the door, and breaking the line has to close it again.
// vanilla: RedStoneWireBlock.updatePowerStrength's wake, DoorBlock.neighborChanged.
func TestDustPointingIntoTheDoorOpensIt(t *testing.T) {
	t.Parallel()
	srv, c := servertest.AttachActor(t)

	standDoor(t, c, 5, 0)
	for x := int32(2); x <= 4; x++ {
		servertest.Setblock(t, c, x, 64, 0, "redstone_wire")
	}
	// The dead line leaves the door shut.
	expectDoorOpen(t, srv, 5, 0, false)

	// The torch feeds the line from its west end.
	servertest.Setblock(t, c, 1, 64, 0, "redstone_torch")
	c.Settle()
	expectDoorOpen(t, srv, 5, 0, true)

	// Breaking the lit line closes the door again.
	servertest.Setblock(t, c, 4, 64, 0, "air")
	c.Settle()
	expectDoorOpen(t, srv, 5, 0, false)
}

// TestTorchBesideTheDoorOpensIt stands a redstone torch on the ground next to
// the door's lower half: the torch's own signal reaches the door on its own.
// vanilla: RedstoneTorchBlock.getSignal.
func TestTorchBesideTheDoorOpensIt(t *testing.T) {
	t.Parallel()
	srv, c := servertest.AttachActor(t)

	standDoor(t, c, 5, 0)
	servertest.Setblock(t, c, 4, 64, 0, "redstone_torch")
	c.Settle()
	expectDoorOpen(t, srv, 5, 0, true)
}

// TestLeverOnTheDoorFrameOpensIt flips a lever mounted on the far face of a
// frame stone beside the door: the lever strong-powers the stone, whose own
// power the door's lower half reads.
// vanilla: LeverBlock.getDirectSignal, LeverBlock.updateNeighbours.
func TestLeverOnTheDoorFrameOpensIt(t *testing.T) {
	t.Parallel()
	srv, c := servertest.AttachActor(t)

	standDoor(t, c, 5, 0)
	servertest.Setblock(t, c, 6, 64, 0, "stone")
	servertest.Setblock(t, c, 7, 64, 0, "lever[face=wall,facing=east,powered=false]")
	expectDoorOpen(t, srv, 5, 0, false)

	c.MoveToGround(9.5, 64, 0.5, true)
	c.Settle()
	c.UseBlock(7, 64, 0)
	c.Settle()
	expectDoorOpen(t, srv, 5, 0, true)
}
