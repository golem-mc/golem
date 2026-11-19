package commands

import (
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// TestAttributeFeedbackUsesTeamDisplayName checks that /attribute, /clear and
// /enchant name a player on a team by the display name the team decorates:
// the prefix and suffix around the name, under the /tell click and hover.
// vanilla: AttributeCommand, ClearInventoryCommands, EnchantCommand
func TestAttributeFeedbackUsesTeamDisplayName(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	_, c := servertest.Join(t, cfg, "Testificate", servertest.OpsListing(t, "Testificate"))

	c.ChatCommand("team add red")
	c.ChatCommand(`team modify red prefix {"text":"[R] "}`)
	c.ChatCommand("team join red")
	servertest.AwaitSystemChatSaying(t, c, "commands.team.join.success.single")

	who := servertest.TeamPlayerDisplayComponent("Testificate", "[R] ", "")

	c.ChatCommand("attribute @s minecraft:max_health get")
	servertest.AwaitCommandFeedbackText(t, c, "commands.attribute.value.get.success",
		protocol.Text{Translate: "attribute.name.max_health"}, who, protocol.Text{Text: "20.0"})

	c.ChatCommand("clear @s")
	servertest.AwaitCommandErrorText(t, c, "clear.failed.single", who)

	// /enchant spells the display name out as plain text.
	c.ChatCommand("enchant @s sharpness 1")
	servertest.AwaitItemFailure(t, c, "commands.enchant.failed.itemless", protocol.Text{Text: "[R] Testificate"})
	c.Disconnect()
}
