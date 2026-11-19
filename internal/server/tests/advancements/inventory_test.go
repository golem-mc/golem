package advancements

import (
	"slices"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/nbt"
)

// vanilla: GiveCommand.giveItem, ServerPlayer.containerListener
func TestGiveAnnouncesAdvancementBeforeFeedback(t *testing.T) {
	t.Parallel()
	cfg := servertest.SurvivalTestConfig()
	cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
	srv, c := joinOp(t, cfg, "Admin")
	servertest.HoldTicks(t, srv)
	c.Settle()
	chatKeys := func(cmd string) []string {
		t.Helper()
		c.ChatCommand(cmd)
		var keys []string
		c.SettleAndCollect(func(id int32, data []byte) {
			if id != protocol.IDPlaySystemChat {
				return
			}
			tag, _, err := nbt.DecodeNetwork(data)
			if err != nil {
				t.Fatal(err)
			}
			keys = append(keys, string(tag.(nbt.Compound)["translate"].(nbt.String)))
		})
		return keys
	}

	for _, name := range []string{"first diamond", "increase revoked stack"} {
		t.Run(name, func(t *testing.T) {
			keys := chatKeys("give @s minecraft:diamond")
			want := []string{"chat.type.advancement.task", "commands.give.success.single"}
			if !slices.Equal(keys, want) {
				t.Errorf("chat order = %v, want %v before the next tick", keys, want)
			}
		})
		if name == "first diamond" {
			servertest.RunTicks(t, srv, 2)
			c.Settle()
			keys := chatKeys("advancement revoke @s only minecraft:story/mine_diamond")
			if !slices.Equal(keys, []string{"commands.advancement.revoke.one.to.one.success"}) {
				t.Fatalf("revoke feedback = %v", keys)
			}
		}
	}
	stack := srv.InventorySlot("Admin", player.HotbarSlot)
	if stack.ItemID != servertest.ItemIDOrDie(t, "minecraft:diamond") || stack.Count != 2 {
		t.Fatalf("hotbar stack = %+v, want two diamonds in the same slot", stack)
	}
}

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
