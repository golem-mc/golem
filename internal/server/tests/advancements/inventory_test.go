package advancements

import (
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// vanilla: InventoryChangeTrigger.TriggerInstance.matches
func TestInventorySinglePredicateRequiresChangedStack(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, cobblestone string }{
		{"give", "give @s minecraft:cobblestone"},
		{"loot", "loot give @s loot minecraft:blocks/stone"},
		{"item", "item replace entity @s hotbar.1 with minecraft:cobblestone"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := servertest.SurvivalTestConfig()
			cfg.ViewDistance = config.MinDistance
			cfg.SimulationDistance = config.MinDistance
			srv, c := joinOp(t, cfg, "Admin")
			servertest.HoldTicks(t, srv)
			c.Settle()
			check := func(granted bool) {
				t.Helper()
				c.ChatCommand("execute if entity @s[advancements={minecraft:story/mine_diamond=true}]")
				if granted {
					count := int64(1)
					c.ExpectCommandFeedbackText("commands.execute.conditional.pass_count", protocol.Text{Number: &count})
				} else {
					c.ExpectCommandFailure(servertest.CommandErrorMessageForTest("commands.execute.conditional.fail"))
				}
			}
			c.ChatCommand("give @s minecraft:diamond")
			c.Settle()
			servertest.RunTicks(t, srv, 2)
			c.Settle()
			check(true)
			c.ChatCommand("advancement revoke @s only minecraft:story/mine_diamond")
			c.Settle()
			c.ChatCommand(tc.cobblestone)
			c.Settle()
			servertest.RunTicks(t, srv, 2)
			c.Settle()
			check(false)
			c.ChatCommand("clear @s minecraft:cobblestone")
			c.Settle()
			servertest.RunTicks(t, srv, 2)
			c.Settle()
			check(false)
			c.ChatCommand("give @s minecraft:diamond")
			c.Settle()
			servertest.RunTicks(t, srv, 2)
			c.Settle()
			check(true)
		})
	}
}
