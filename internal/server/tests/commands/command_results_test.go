package commands_test

import (
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// TestCommandsResultIsTheirCount checks that /execute store result takes the
// count a command answers rather than 1: the entities killed, the blocks
// filled and tagged, the players given an item.
// vanilla: KillCommand.kill, FillCommand.fillBlocks, TagCommand.addTag, GiveCommand.giveItem
func TestCommandsResultIsTheirCount(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
	_, c := servertest.Join(t, cfg, "Alice", servertest.OpsListing(t, "Alice"))

	for range 3 {
		c.ChatCommand("summon minecraft:cow 2.5 70 2.5")
		cow := c.ExpectMobSpawn(protocol.EntityTypeCow)
		c.ExpectCommandFeedbackText("commands.summon.success",
			servertest.EntityDisplayComponent("minecraft:cow", "entity.minecraft.cow", cow.Uuid))
	}

	c.ChatCommand("execute store result storage t:a t int 1 run tag @e[type=minecraft:cow] add x")
	c.ExpectCommandFeedbackText("commands.tag.add.success.multiple", protocol.Text{Text: "x"}, servertest.NumberText(3))
	c.ChatCommand("data get storage t:a t")
	c.ExpectCommandFeedback("commands.data.storage.query", "t:a", "3")

	c.ChatCommand("execute store result storage t:a k int 1 run kill @e[type=minecraft:cow]")
	c.ExpectCommandFeedbackText("commands.kill.success.multiple", servertest.NumberText(3))
	c.ChatCommand("data get storage t:a k")
	c.ExpectCommandFeedback("commands.data.storage.query", "t:a", "3")

	c.ChatCommand("execute store result storage t:a f int 1 run fill 0 70 0 1 70 1 minecraft:stone")
	c.ExpectCommandFeedbackText("commands.fill.success", servertest.NumberText(4))
	c.ChatCommand("data get storage t:a f")
	c.ExpectCommandFeedback("commands.data.storage.query", "t:a", "4")
}
