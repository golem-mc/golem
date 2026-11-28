package redstone

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"
)

// A torch that turns off notifies its neighbours' neighbours before its own,
// so the wall torch above it books its tick ahead of the one two blocks east.
// Both run on the same tick; the first re-lights the wire, so the second
// finds the block it sits on powered again and stays off.
// The wire's wake order follows block-position hashes, so the circuit stands
// at coordinates where the unfixed order books the far torch first.
// vanilla: RedstoneTorchBlock.onPlace, LevelChunk.setBlockState, Level.setBlock
func TestTorchTurnOffNotifiesRingBeforeOwnNeighbours(t *testing.T) {
	t.Parallel()
	srv, c := servertest.AttachActor(t)
	servertest.HoldTicks(t, srv)
	const x, y, z = 173, 57, 269
	c.ChatCommand("forceload add 173 269 179 269")
	servertest.RunTicks(t, srv, 4)
	for _, b := range []struct {
		dx, dy, dz int32
		block      string
	}{
		{0, -1, 0, "orange_concrete"},
		{0, 0, 0, "redstone_torch"},
		{0, 1, 0, "orange_concrete"},
		{1, 1, 0, "redstone_wall_torch[facing=east]"},
		{1, -1, 0, "orange_concrete"},
		{1, 0, 0, "redstone_wire"},
		{2, 0, 0, "orange_concrete"},
		{3, 0, 0, "redstone_wall_torch[facing=east]"},
		{0, -1, -1, "repeater[facing=north]"},
	} {
		servertest.Setblock(t, c, x+b.dx, y+b.dy, z+b.dz, b.block)
	}
	servertest.RunTicks(t, srv, 20)
	servertest.ExpectBlockNamed(t, srv, x+3, y, z, "minecraft:redstone_wall_torch", map[string]string{"facing": "east", "lit": "false"})

	// The repeater's input powers the concrete under the standing torch.
	servertest.Setblock(t, c, x, y-1, z-2, "redstone_block")
	for range 12 {
		servertest.RunTicks(t, srv, 1)
		servertest.ExpectBlockNamed(t, srv, x+3, y, z, "minecraft:redstone_wall_torch", map[string]string{"facing": "east", "lit": "false"})
	}
	servertest.ExpectBlockNamed(t, srv, x, y, z, "minecraft:redstone_torch", map[string]string{"lit": "false"})
}
