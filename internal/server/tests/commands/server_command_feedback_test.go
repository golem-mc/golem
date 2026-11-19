package commands

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/registry"
)

// A successful command with logging allowed hands its line to the other
// operators as vanilla's CommandSourceStack.broadcastToAdmins builds it: a
// grey italic [Name: message] chat.type.admin line, the runner left out, gated
// by send_command_feedback.

// stoneGiveFeedback is the line /give @s stone answers with: one stone, its
// display name, and the display name of whoever received it.
func stoneGiveFeedback(t *testing.T, to protocol.Text) protocol.Text {
	t.Helper()
	stone, ok := registry.ItemID("minecraft:stone")
	if !ok {
		t.Fatal("the registry has no stone")
	}
	key, ok := registry.ItemTranslationKey(stone)
	if !ok {
		t.Fatal("stone has no translation key")
	}
	count := int64(1)
	return protocol.Text{
		Translate: "commands.give.success.single",
		With: []protocol.Text{
			{Number: &count},
			{
				Translate: "chat.square_brackets",
				Color:     registry.RarityOf(stone).Color(),
				With:      []protocol.Text{{Extra: []protocol.Text{{Translate: key}}}},
				HoverItem: &protocol.HoverItem{ID: "minecraft:stone", Count: 1},
			},
			to,
		},
	}
}

// TestCommandFeedbackBroadcastsToOperators has two operators and one plain
// player in the world: the runner's /give reaches the other operator as the
// grey [Name: message] line, while the runner and the plain player read
// nothing more of it.
func TestCommandFeedbackBroadcastsToOperators(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	_, connect := servertest.Start(t, cfg, servertest.OpsListing(t, "Alice", "Bob"))

	alice := servertest.JoinFeedbackWorld(t, connect, "Alice", nil)
	bob := servertest.JoinFeedbackWorld(t, connect, "Bob", []*servertest.Client{alice})
	charlie := servertest.JoinFeedbackWorld(t, connect, "Charlie", []*servertest.Client{alice, bob})

	alice.ChatCommand("give @s stone")
	feedback := stoneGiveFeedback(t, servertest.PlayerDisplayComponent("Alice"))
	servertest.AwaitCommandFeedbackText(t, alice, "commands.give.success.single", feedback.With...)
	// The broadcast is written as the runner's command connection carries on,
	// so a ping behind it settles what the others should have.
	alice.Settle()

	servertest.AwaitAdminBroadcast(t, bob, servertest.PlayerDisplayComponent("Alice"), feedback)
	servertest.AwaitNoSystemChat(t, alice)
	servertest.AwaitNoSystemChat(t, charlie)

	alice.Disconnect()
	bob.Disconnect()
	charlie.Disconnect()
}

// TestCommandFailuresWearTheErrorForm has an operator run the commands that
// fail without changing anything - an /clear of an empty inventory, and /op and /deop that change nothing - and checks
// each line is the red sendFailure form rather than white success feedback.
// send_command_feedback gates successes alone, so the same failures arrive
// with the rule both on and off.
func TestCommandFailuresWearTheErrorForm(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	_, c := servertest.Join(t, cfg, "Testificate", servertest.OpsListing(t, "Testificate"))

	expectFailures := func() {
		c.ChatCommand("clear @s")
		c.ExpectCommandFailure(servertest.CommandErrorMessageTextForTest("clear.failed.single", servertest.PlayerDisplayComponent("Testificate")))
		c.ChatCommand("op Testificate")
		c.ExpectCommandFailure(servertest.CommandErrorMessageForTest("commands.op.failed"))
		c.ChatCommand("deop Bob")
		c.ExpectCommandFailure(servertest.CommandErrorMessageForTest("commands.deop.failed"))
	}

	expectFailures()

	c.ChatCommand("gamerule send_command_feedback false")
	c.Settle()
	expectFailures()

	c.Disconnect()
}

// TestFeedbackRuleOffSilencesBroadcast turns send_command_feedback off and has
// the operator /give: the other operator reads nothing, as vanilla gates the
// broadcast and the runner's own reply on the same rule.
func TestFeedbackRuleOffSilencesBroadcast(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	_, connect := servertest.Start(t, cfg, servertest.OpsListing(t, "Alice", "Bob"))

	alice := servertest.JoinFeedbackWorld(t, connect, "Alice", nil)
	bob := servertest.JoinFeedbackWorld(t, connect, "Bob", []*servertest.Client{alice})
	charlie := servertest.JoinFeedbackWorld(t, connect, "Charlie", []*servertest.Client{alice, bob})

	alice.ChatCommand("gamerule send_command_feedback false")
	alice.Settle()
	alice.ChatCommand("give @s stone")
	alice.Settle()

	servertest.AwaitNoSystemChat(t, bob)
	servertest.AwaitNoSystemChat(t, charlie)

	alice.Disconnect()
	bob.Disconnect()
	charlie.Disconnect()
}
