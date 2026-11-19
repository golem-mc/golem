package commands

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"
)

// A record component named in /give is stored and read back through /data.
func TestGiveKeepsCookingFuelComponent(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Alice", servertest.OpsListing(t, "Alice"))
	servertest.HoldTicks(t, srv)
	c.ChatCommand(`give @s stick[cooking_fuel={burn_time:200,speed_multiplier:2.0f}]`)
	servertest.AwaitSystemChatSaying(t, c, "commands.give.success.single")
	c.Settle()
	c.ChatCommand(`data get entity @s SelectedItem.components."minecraft:cooking_fuel"`)
	line := servertest.FeedbackComponent(t, c)
	if len(line.With) < 2 || line.With[len(line.With)-1].Plain() != "{burn_time: 200, speed_multiplier: 2.0f}" {
		t.Fatalf("cooking_fuel read back as %s", line.Plain())
	}
}
