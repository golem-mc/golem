package commands

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
)

// TestSetblockRefusesAPositionOutsideTheWorld checks a /setblock position
// outside the world, past its thirty million blocks or above it: refused
// before anything is read or written, as unloaded where its chunk is not
// loaded, the edge of the int32 range among them, and as out of the world
// where it is. The command after them still runs.
// vanilla: BlockPosArgument.getLoadedBlockPos
func TestSetblockRefusesAPositionOutsideTheWorld(t *testing.T) {
	t.Parallel()
	srv, c := setblockActor(t)

	for _, cmd := range []string{
		"setblock 2147483647 64 2147483647 stone",
		"setblock -2147483648 64 -2147483648 stone keep",
		"setblock 30000000 64 2000 stone",
	} {
		c.ChatCommand(cmd)
		servertest.AwaitCommandFailure(t, c, "argument.pos.unloaded")
	}
	for _, at := range [][2]int32{{2147483647, 2147483647}, {-2147483648, -2147483648}, {30000000, 2000}} {
		if srv.ChunkLoadedInDimension(world.DimensionName, at[0], at[1]) {
			t.Errorf("a refused /setblock loaded the chunk at (%d, %d)", at[0], at[1])
		}
	}

	c.ChatCommand("setblock 0 400 0 stone")
	servertest.AwaitCommandFailure(t, c, "argument.pos.outofworld")

	// A loaded chunk past the horizontal limit is out of the world, not
	// merely unloaded; /fill's corners are held to the same check.
	release := srv.HoldChunk(30000000, 0)
	defer release()
	c.ChatCommand("setblock 30000000 64 0 stone")
	servertest.AwaitCommandFailure(t, c, "argument.pos.outofworld")
	c.ChatCommand("fill 30000000 64 0 30000000 64 0 stone")
	servertest.AwaitCommandFailure(t, c, "argument.pos.outofworld")

	c.ChatCommand("setblock 0 70 0 stone")
	servertest.AwaitCommandFeedback(t, c, "commands.setblock.success", "0", "70", "0")
	c.Disconnect()
}
