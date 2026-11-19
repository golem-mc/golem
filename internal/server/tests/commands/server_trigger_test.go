package commands

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"
)

// TestTriggerCommand runs /trigger: each run needs the score enabled, locks
// it again, and refuses an objective that is not a trigger.
func TestTriggerCommand(t *testing.T) {
	t.Parallel()
	_, c := servertest.StartExecuteWorld(t, "Alice")

	c.ChatCommand("scoreboard objectives add press trigger")
	expectScoreboardFeedback(t, c, "commands.scoreboard.objectives.add.success")
	c.ChatCommand("scoreboard objectives add points dummy")
	expectScoreboardFeedback(t, c, "commands.scoreboard.objectives.add.success")

	c.ChatCommand("trigger press")
	expectScoreboardRefusal(t, c, "commands.trigger.failed.unprimed")
	c.ChatCommand("trigger nope")
	expectScoreboardRefusal(t, c, "arguments.objective.notFound")
	c.ChatCommand("trigger points")
	expectScoreboardRefusal(t, c, "commands.trigger.failed.invalid")

	enable := func() {
		t.Helper()
		c.ChatCommand("scoreboard players enable Alice press")
		expectScoreboardFeedback(t, c, "commands.scoreboard.players.enable.success.single")
	}
	get := func(want int64) {
		t.Helper()
		c.ChatCommand("scoreboard players get Alice press")
		with := expectScoreboardFeedback(t, c, "commands.scoreboard.players.get.success")
		if len(with) != 3 || number(with[1]) != want {
			t.Errorf("get arguments %+v, want %d", with, want)
		}
	}

	enable()
	c.ChatCommand("trigger press")
	expectScoreboardFeedback(t, c, "commands.trigger.simple.success")
	get(1)
	c.ChatCommand("trigger press")
	expectScoreboardRefusal(t, c, "commands.trigger.failed.unprimed")

	enable()
	c.ChatCommand("trigger press add 5")
	if with := expectScoreboardFeedback(t, c, "commands.trigger.add.success"); len(with) != 2 || number(with[1]) != 5 {
		t.Errorf("add arguments %+v, want an amount of 5", with)
	}
	get(6)

	enable()
	c.ChatCommand("trigger press set -2")
	if with := expectScoreboardFeedback(t, c, "commands.trigger.set.success"); len(with) != 2 || number(with[1]) != -2 {
		t.Errorf("set arguments %+v, want -2", with)
	}
	get(-2)
	c.ChatCommand("trigger press set 4")
	expectScoreboardRefusal(t, c, "commands.trigger.failed.unprimed")
	get(-2)

	c.Disconnect()
}
