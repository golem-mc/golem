package commands

import (
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/nbt"
)

// createMovingPiston pushes a block up into the target cell, leaving its real
// moving entity available for command loads while the loop is held.
func createMovingPiston(t *testing.T, srv *server.Server, c *servertest.Client, x, y, z int32) {
	t.Helper()
	servertest.Setblock(t, c, x, y-2, z, "piston[facing=up]")
	servertest.Setblock(t, c, x, y-1, z, "diamond_block")
	servertest.Setblock(t, c, x, y-2, z+1, "redstone_block")
	servertest.RunTicks(t, srv, 1)
	servertest.ExpectBlockNamed(t, srv, x, y, z, "minecraft:moving_piston", nil)
	tag, ok := srv.BlockEntityTagAt(x, y, z)
	if !ok || tag["blockState"] != nbt.String("minecraft:diamond_block") {
		t.Fatalf("piston movement did not create its moving entity: %v", tag)
	}
}

// vanilla: MovingPistonBlock.newBlockEntity, BlockInput.place
func TestMovingPistonPlacementHasNoBlockEntity(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
	srv, c := servertest.Join(t, cfg, "Alice", servertest.OpsListing(t, "Alice"), func(s *server.Server) { servertest.FreezeUntilHeld(t, s) })
	servertest.HoldTicks(t, srv)
	const tag = `{blockState:{id:"minecraft:oak_stairs",properties:{facing:"east",half:"top",shape:"straight",waterlogged:"true"}},facing:0b,extending:1b,source:0b}`
	for _, placement := range []struct {
		command, feedback string
	}{
		{"setblock 10 64 3 moving_piston" + tag, "commands.setblock.success"},
		{"setblock 10 64 3 moving_piston" + tag + " strict", "commands.setblock.success"},
		{"fill 10 64 3 10 64 3 moving_piston" + tag, "commands.fill.success"},
		{"setblock 10 64 3 moving_piston", "commands.setblock.success"},
	} {
		servertest.Setblock(t, c, 10, 64, 3, "stone")
		c.ChatCommand(placement.command)
		servertest.AwaitSystemChatSaying(t, c, placement.feedback)
		if entity, ok := srv.BlockEntityTagAt(10, 64, 3); ok {
			t.Errorf("%s created a block entity: %v", placement.command, entity)
		}
		for _, plain := range []string{"", ",plain:1b"} {
			c.ChatCommand(`tellraw @s {nbt:"blockState",block:"10 64 3"` + plain + `}`)
			if got := servertest.FeedbackComponent(t, c).Plain(); got != "" {
				t.Errorf("%s: NBT component = %q, want empty", placement.command, got)
			}
		}
		for _, command := range []string{
			"data get block 10 64 3",
			"data get block 10 64 3 blockState",
			"data merge block 10 64 3 {facing:2b}",
		} {
			c.ChatCommand(command)
			servertest.AwaitCommandError(t, c, "commands.data.block.invalid")
		}
		// An unchanged state cannot succeed by loading NBT into a missing entity.
		c.ChatCommand("setblock 10 64 3 moving_piston" + tag)
		servertest.AwaitCommandError(t, c, "commands.setblock.failed")
	}
}
