package commands

import (
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
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

// TestSpreadPlayersFailsAroundACentreOutsideTheWorld checks a centre past
// the world's horizontal limits, one so large it is infinite as a float
// among them: every column there reads as void air with no floor to stand
// on, so the spread fails with the entities failure and moves nobody.
// vanilla: SpreadPlayersCommand.spreadPlayers, Level.getBlockState
func TestSpreadPlayersFailsAroundACentreOutsideTheWorld(t *testing.T) {
	t.Parallel()
	srv, c := setblockActor(t)
	before := srv.PlayerPosition("Testificate")

	one, half := int64(1), float32(0.5)
	for _, centre := range []struct {
		word string
		x    float32
	}{
		{"1000000000000000000000000000000000000000", float32(math.Inf(1))},
		{"40000000", float32(40000000.5)},
	} {
		c.ChatCommand("spreadplayers " + centre.word + " 0 1 10 false @s")
		x := centre.x
		servertest.AwaitCommandErrorText(t, c, "commands.spreadplayers.failed.entities",
			protocol.Text{Number: &one}, protocol.Text{Float: &x}, protocol.Text{Float: &half},
			protocol.Text{Text: "0.00"})
	}
	if p := srv.PlayerPosition("Testificate"); p.X != before.X || p.Z != before.Z {
		t.Errorf("a failed spread moved the player from (%v, %v) to (%v, %v)", before.X, before.Z, p.X, p.Z)
	}

	c.ChatCommand("spreadplayers 0 0 1 10 false @s")
	servertest.AwaitCommandFeedbackText(t, c, "commands.spreadplayers.success.entities",
		protocol.Text{Number: &one}, protocol.Text{Float: &half}, protocol.Text{Float: &half},
		protocol.Text{Text: "0.00"})
	c.Disconnect()
}

// TestSpawnPositionsOutsideTheBoundsAreRefused checks the spawnable check
// /setworldspawn and /spawnpoint run on their position, a relative offset too
// large for a block coordinate among them, and that the spawn stays put.
// vanilla: BlockPosArgument.getSpawnablePos
func TestSpawnPositionsOutsideTheBoundsAreRefused(t *testing.T) {
	t.Parallel()
	_, c := setblockActor(t)

	for _, cmd := range []string{
		"setworldspawn 30000000 64 0",
		"setworldspawn 0 64 -30000001",
		"setworldspawn 0 20000000 0",
		"setworldspawn ~10000000000 64 0",
		"spawnpoint @s 30000000 64 0",
		"spawnpoint @s 0 -20000001 0",
		"spawnpoint @s ~ ~ ~-10000000000",
	} {
		c.ChatCommand(cmd)
		servertest.AwaitCommandFailure(t, c, "argument.pos.outofbounds")
	}

	c.ChatCommand("setworldspawn 5 64 5")
	servertest.AwaitCommandFeedback(t, c, "commands.setworldspawn.success",
		"5", "64", "5", "0.0", "0.0", "minecraft:overworld")
	c.Disconnect()
}

// TestTeleportRefusesARelativeOffsetPastTheWorld checks a relative /tp
// offset too large for a block coordinate: it is outside the world, not
// wrapped back inside it.
// vanilla: TeleportCommand.performTeleport, Level.isInSpawnableBounds
func TestTeleportRefusesARelativeOffsetPastTheWorld(t *testing.T) {
	t.Parallel()
	srv, c := setblockActor(t)
	before := srv.PlayerPosition("Testificate")

	for _, cmd := range []string{
		"tp @s ~10000000000 ~ ~",
		"tp @s ~ ~ ~-10000000000",
		"teleport @s ~4294967296 ~ ~",
	} {
		c.ChatCommand(cmd)
		servertest.AwaitCommandError(t, c, "commands.teleport.invalidPosition")
	}
	if p := srv.PlayerPosition("Testificate"); p.X != before.X || p.Z != before.Z {
		t.Errorf("a refused teleport moved the player from (%v, %v) to (%v, %v)", before.X, before.Z, p.X, p.Z)
	}
	c.Disconnect()
}

// TestSpreadplayersNeverLandsOnShortGrass checks the floor a spread may land
// on is in entities_can_teleport_to: short grass covering every column of the
// range fails the spread, and one bare column is where the player lands.
// vanilla: SpreadPlayersCommand.Position.isSafe
func TestSpreadplayersNeverLandsOnShortGrass(t *testing.T) {
	t.Parallel()
	srv, c := setblockActor(t)

	// Range 2 around the middle of block (0, 0) covers blocks -2..2.
	one, filled, half := int64(1), int64(25), float32(0.5)
	// Short grass does not survive on the test world's stone floor.
	c.ChatCommand("fill -2 63 -2 2 63 2 dirt")
	servertest.AwaitCommandFeedbackText(t, c, "commands.fill.success", protocol.Text{Number: &filled})
	c.ChatCommand("fill -2 64 -2 2 64 2 short_grass")
	servertest.AwaitCommandFeedbackText(t, c, "commands.fill.success", protocol.Text{Number: &filled})

	c.ChatCommand("spreadplayers 0 0 1 2 false @s")
	servertest.AwaitCommandErrorText(t, c, "commands.spreadplayers.failed.entities",
		protocol.Text{Number: &one}, protocol.Text{Float: &half}, protocol.Text{Float: &half},
		protocol.Text{Text: "0.00"})

	c.ChatCommand("setblock 1 64 -1 air")
	servertest.AwaitCommandFeedback(t, c, "commands.setblock.success", "1", "64", "-1")
	c.ChatCommand("spreadplayers 0 0 1 2 false @s")
	servertest.AwaitCommandFeedbackText(t, c, "commands.spreadplayers.success.entities",
		protocol.Text{Number: &one}, protocol.Text{Float: &half}, protocol.Text{Float: &half},
		protocol.Text{Text: "0.00"})
	if p := srv.PlayerPosition("Testificate"); p.X != 1.5 || p.Y != 64 || p.Z != -0.5 {
		t.Errorf("player at (%v, %v, %v), want the bare column's floor at (1.5, 64, -0.5)", p.X, p.Y, p.Z)
	}
	c.Disconnect()
}
