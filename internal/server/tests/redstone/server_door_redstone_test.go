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

// vanilla: DoorBlock.updateShape, DoorBlock.canSurvive
func TestPoplarDoorBreaksWithoutSupport(t *testing.T) {
	t.Parallel()
	srv, c := servertest.AttachActor(t)
	standDoorNamed(t, c, "poplar_door", 5, 0)
	servertest.Setblock(t, c, 5, 63, 0, "air")
	servertest.ExpectBlockNamed(t, srv, 5, 64, 0, "minecraft:air", nil)
	servertest.ExpectBlockNamed(t, srv, 5, 65, 0, "minecraft:air", nil)
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

// TestDustRunningPastTheDoorLeavesItShut checks the negative: a powered line
// crossing beside the door connects neither end toward it, so the door its
// dust runs past stays shut.
// vanilla: RedStoneWireBlock.getSignal's connection check.
func TestDustRunningPastTheDoorLeavesItShut(t *testing.T) {
	t.Parallel()
	srv, c := servertest.AttachActor(t)

	standDoor(t, c, 5, 0)
	// A north-south line whose middle cell is the door's western neighbour.
	servertest.Setblock(t, c, 4, 64, -3, "redstone_torch")
	for z := int32(-2); z <= 0; z++ {
		servertest.Setblock(t, c, 4, 64, z, "redstone_wire")
	}
	c.Settle()

	servertest.ExpectBlockNamed(t, srv, 4, 64, 0, "minecraft:redstone_wire", map[string]string{"power": "13"})
	expectDoorOpen(t, srv, 5, 0, false)
}

// TestDustBesideTheBlockBelowTheDoorOpensIt buries the dust in the ground
// beside the threshold, pointing east into the block the door stands on:
// the wire strongly powers that block and the door reads the power through
// it. The torch goes in last, so the wake has to reach the door.
// vanilla: RedStoneWireBlock.getDirectSignal, SignalGetter.getDirectSignalTo.
func TestDustBesideTheBlockBelowTheDoorOpensIt(t *testing.T) {
	t.Parallel()
	srv, c := servertest.AttachActor(t)

	standDoor(t, c, 5, 0)
	// The trench replaces the ground cells west of the threshold at y=63.
	servertest.Setblock(t, c, 3, 63, 0, "redstone_wire")
	servertest.Setblock(t, c, 4, 63, 0, "redstone_wire")
	expectDoorOpen(t, srv, 5, 0, false)

	servertest.Setblock(t, c, 2, 63, 0, "redstone_torch")
	c.Settle()
	expectDoorOpen(t, srv, 5, 0, true)
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

// TestTorchBesideAnIronDoorOpensIt powers an iron door, which a hand cannot
// open: its only way open is the redstone neighborChanged path.
// vanilla: IronDoorBlock, DoorBlock.neighborChanged.
func TestTorchBesideAnIronDoorOpensIt(t *testing.T) {
	t.Parallel()
	srv, c := servertest.AttachActor(t)

	standDoorNamed(t, c, "iron_door", 5, 0)
	servertest.Setblock(t, c, 4, 64, 0, "redstone_torch")
	c.Settle()
	expectDoorOpenNamed(t, srv, "minecraft:iron_door", 5, 0, true)
}

// TestWallTorchOnTheBlockBelowLeavesTheDoorShut checks the trap: a wall torch
// hanging on the threshold block strong-powers the empty cell above its own
// cell — beside the door, not the door itself — so the door above stays shut.
// vanilla: RedstoneTorchBlock.getDirectSignal (direction DOWN only).
func TestWallTorchOnTheBlockBelowLeavesTheDoorShut(t *testing.T) {
	t.Parallel()
	srv, c := servertest.AttachActor(t)

	standDoor(t, c, 5, 0)
	// A trench cell west of the threshold, with the torch hanging on it.
	servertest.Setblock(t, c, 4, 63, 0, "redstone_wall_torch[facing=west,lit=true]")
	c.Settle()
	servertest.ExpectBlockNamed(t, srv, 4, 63, 0, "minecraft:redstone_wall_torch", map[string]string{"lit": "true"})
	expectDoorOpen(t, srv, 5, 0, false)
}

// TestTorchInThePitUnderTheDoorOpensIt stands a redstone torch in a pit
// directly below the threshold: the torch strong-powers the block above its
// own cell, which is the threshold itself, and the door reads that power —
// the contrast to the wall torch above, which reaches nothing.
// vanilla: RedstoneTorchBlock.getDirectSignal, SignalGetter.getDirectSignalTo.
func TestTorchInThePitUnderTheDoorOpensIt(t *testing.T) {
	t.Parallel()
	srv, c := servertest.AttachActor(t)

	standDoor(t, c, 5, 0)
	// The pit is the cell directly below the threshold, whose stone the
	// torch replaces; the door above keeps its support.
	servertest.Setblock(t, c, 5, 62, 0, "redstone_torch")
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

// TestDustBesideTheUpperHalfOpensIt points a raised line of dust into the
// door's upper half: the wire's wake reaches that half, whose signal opens
// the pair. The glass ledge below the line does not conduct, so the lower
// half cannot be powered through it, and a later wake beside the lower half
// has to leave the door open by reading the powered upper half.
// vanilla: DoorBlock.neighborChanged's hasNeighborSignal over both halves.
func TestDustBesideTheUpperHalfOpensIt(t *testing.T) {
	t.Parallel()
	srv, c := servertest.AttachActor(t)

	standDoor(t, c, 5, 0)
	// A glass ledge carries the line at the upper half's level.
	for x := int32(1); x <= 4; x++ {
		servertest.Setblock(t, c, x, 64, 0, "glass")
	}
	for x := int32(2); x <= 4; x++ {
		servertest.Setblock(t, c, x, 65, 0, "redstone_wire")
	}
	expectDoorOpen(t, srv, 5, 0, false)

	servertest.Setblock(t, c, 1, 65, 0, "redstone_torch")
	c.Settle()
	expectDoorOpen(t, srv, 5, 0, true)

	// The wake beside the lower half makes it re-read its neighbours; only
	// the upper half is powered, and reading it keeps the pair open.
	servertest.Setblock(t, c, 5, 64, 1, "stone")
	c.Settle()
	expectDoorOpen(t, srv, 5, 0, true)
}
