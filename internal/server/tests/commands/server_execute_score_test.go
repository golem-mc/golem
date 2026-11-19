package commands

import (
	"bytes"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// scoreTestPasses runs an execute score test as a whole command and answers
// whether the chain's end reported a pass or the conditional failure.
func scoreTestPasses(t *testing.T, c *servertest.Client, command string) bool {
	t.Helper()
	c.ChatCommand(command)
	got := c.ExpectPlay(protocol.IDPlaySystemChat)
	switch {
	case bytes.Equal(got, plainFeedbackTextsForTest("commands.execute.conditional.pass")):
		return true
	case bytes.Equal(got, servertest.CommandErrorMessageForTest("commands.execute.conditional.fail")):
		return false
	}
	t.Fatalf("%q answered % X, want the conditional pass or failure", command, got)
	return false
}

// TestExecuteScoreConditionalReadsTheScoreboard checks the score test against
// real scores: the comparisons, the matches range, a holder without a score,
// the unless form and the refusals for a missing objective.
func TestExecuteScoreConditionalReadsTheScoreboard(t *testing.T) {
	t.Parallel()
	_, c := servertest.StartExecuteWorld(t, "Testificate")

	c.ChatCommand("scoreboard objectives add points dummy")
	expectScoreboardFeedback(t, c, "commands.scoreboard.objectives.add.success")
	for _, set := range []string{"players set Alice points 5", "players set Bob points 8"} {
		c.ChatCommand("scoreboard " + set)
		expectScoreboardFeedback(t, c, "commands.scoreboard.players.set.success.single")
	}

	cases := []struct {
		command string
		want    bool
	}{
		{"execute if score Alice points = Alice points", true},
		{"execute if score Alice points = Bob points", false},
		{"execute if score Alice points < Bob points", true},
		{"execute if score Bob points < Alice points", false},
		{"execute if score Alice points < Alice points", false},
		{"execute if score Alice points > Alice points", false},
		{"execute if score Alice points <= Alice points", true},
		{"execute if score Bob points > Alice points", true},
		{"execute if score Alice points > Bob points", false},
		{"execute if score Bob points >= Bob points", true},
		{"execute if score Alice points >= Bob points", false},
		{"execute if score Alice points matches 5", true},
		{"execute if score Alice points matches 6", false},
		{"execute if score Alice points matches 1..5", true},
		{"execute if score Alice points matches ..4", false},
		{"execute if score Bob points matches 9..", false},
		{"execute if score Bob points matches 8..", true},
		// A holder with no score fails the test, without an error.
		{"execute if score Nobody points matches 0..", false},
		{"execute if score Alice points = Nobody points", false},
		{"execute if score Nobody points = Alice points", false},
		{"execute unless score Nobody points matches 0..", true},
		{"execute unless score Alice points = Bob points", true},
		{"execute unless score Alice points = Alice points", false},
	}
	for _, tc := range cases {
		if got := scoreTestPasses(t, c, tc.command); got != tc.want {
			t.Errorf("%q passed = %v, want %v", tc.command, got, tc.want)
		}
	}

	// The source objective refuses even when the target has no score.
	c.ChatCommand("execute if score Nobody points = Alice missing")
	if got := c.ExpectPlay(protocol.IDPlaySystemChat); !bytes.Equal(got, servertest.CommandErrorMessageForTest("arguments.objective.notFound", "missing")) {
		t.Errorf("the missing source objective answered % X, want the unknown objective", got)
	}
	c.ChatCommand("execute if score Alice missing matches 0..")
	if got := c.ExpectPlay(protocol.IDPlaySystemChat); !bytes.Equal(got, servertest.CommandErrorMessageForTest("arguments.objective.notFound", "missing")) {
		t.Errorf("the missing target objective answered % X, want the unknown objective", got)
	}
	c.Disconnect()
}
