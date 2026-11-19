package commands

import (
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
)

func TestGiveUsesItemDescriptionPrefix(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.GameMode = "creative"
	_, c := servertest.Join(t, cfg, "Testificate", servertest.OpsListing(t, "Testificate"))
	for _, name := range []string{"wheat", "nether_wart", "resin_clump"} {
		c.ChatCommand("give @s minecraft:" + name)
		one := int64(1)
		servertest.AwaitCommandFeedbackText(t, c, "commands.give.success.single",
			protocol.Text{Number: &one},
			protocol.Text{
				Translate: "chat.square_brackets",
				Color:     "white",
				With:      []protocol.Text{{Extra: []protocol.Text{{Translate: "item.minecraft." + name}}}},
				HoverItem: &protocol.HoverItem{ID: "minecraft:" + name, Count: 1},
			},
			servertest.PlayerDisplayComponent("Testificate"))
	}
	c.Disconnect()
}
