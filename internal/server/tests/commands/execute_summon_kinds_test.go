package commands

import (
	"bytes"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// TestExecuteSummonMakesEveryKindTheExecutor checks that an execute summon
// step hands the chain the new body as @s for the non-mob kinds too.
// vanilla: ExecuteCommand.spawnEntityAndRedirect
func TestExecuteSummonMakesEveryKindTheExecutor(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.ViewDistance = config.MinDistance
	cfg.SimulationDistance = config.MinDistance

	srv, c := servertest.Join(t, cfg, "Testificate", servertest.OpsListing(t, "Testificate"))
	servertest.HoldTicks(t, srv)
	c.MoveToGround(12.5, 64, 8.5, true)
	c.Settle()

	for _, kind := range []string{"marker", "armor_stand", "text_display", "oak_boat", "minecart", "end_crystal"} {
		c.ChatCommand("execute positioned 3 64 3 summon minecraft:" + kind + " run tag @s add made")
		if got := c.ExpectSystemChatPastMobs(); !bytes.Contains(got, []byte("commands.tag.add.success.single")) {
			t.Errorf("%s: the tag answered % X, want commands.tag.add.success.single", kind, got)
		}
		c.ChatCommand("execute if entity @e[type=minecraft:" + kind + ",tag=made]")
		if got := c.ExpectSystemChatPastMobs(); !bytes.Equal(got, conditionalPassCountForTest(1)) {
			t.Errorf("%s: the tagged body test answered % X, want one body counted", kind, got)
		}
	}
	// An empty item is no @e result afterwards, yet the chain runs on it.
	c.ChatCommand("execute positioned 3 64 3 summon minecraft:item run tag @s add made")
	if got := c.ExpectSystemChatPastMobs(); !bytes.Contains(got, []byte("commands.tag.add.success.single")) {
		t.Errorf("item: the tag answered % X, want commands.tag.add.success.single", got)
	}
	c.Disconnect()
}
