package commands

import (
	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"
)

// TestGiveCarriesMobVariantComponents checks that /give takes a dye enum and a
// registry holder component of a spawn egg and /data reads them back.
// vanilla: DataComponents.SHEEP_COLOR, DataComponents.VILLAGER_VARIANT
func TestGiveCarriesMobVariantComponents(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Alice", servertest.OpsListing(t, "Alice"))
	servertest.HoldTicks(t, srv)
	for _, tc := range []struct{ give, component, want string }{
		{`sheep_spawn_egg[sheep/color="red"]`, `"minecraft:sheep/color"`, `"red"`},
		{`villager_spawn_egg[villager/variant="minecraft:snow"]`, `"minecraft:villager/variant"`, `"minecraft:snow"`},
	} {
		c.ChatCommand("clear @s")
		c.Settle()
		c.ChatCommand("give @s " + tc.give)
		servertest.AwaitSystemChatSaying(t, c, "commands.give.success.single")
		c.Settle()
		c.ChatCommand("data get entity @s SelectedItem.components." + tc.component)
		line := servertest.FeedbackComponent(t, c)
		if !strings.Contains(line.Plain(), tc.want) {
			t.Errorf("%s read back as %q, want %s", tc.give, line.Plain(), tc.want)
		}
	}
}
