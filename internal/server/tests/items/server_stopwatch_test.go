package items

import (
	"bytes"
	"github.com/golem-mc/golem/internal/server/servertest"
	"strconv"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
)

// TestStopwatchCommand runs the /stopwatch command's four subcommands and
// checks vanilla's translation keys: a create and a repeat create, a query
// whose seconds carry the millisecond precision, a restart and a remove, and
// the does-not-exist refusal every subcommand but create gives a missing id.
func TestStopwatchCommand(t *testing.T) {
	t.Parallel()
	_, c := servertest.StartExecuteWorld(t, "Testificate")

	c.ChatCommand("stopwatch create foo:bar")
	if got := c.ExpectPlay(protocol.IDPlaySystemChat); !bytes.Equal(got, servertest.PlainFeedbackTextsForTest("commands.stopwatch.create.success", "foo:bar")) {
		t.Errorf("create answered % X, want the created notice", got)
	}

	c.ChatCommand("stopwatch create foo:bar")
	if got := c.ExpectPlay(protocol.IDPlaySystemChat); !bytes.Equal(got, servertest.CommandErrorMessageForTest("commands.stopwatch.already_exists", "foo:bar")) {
		t.Errorf("repeat create answered % X, want the already-exists refusal", got)
	}

	// A query shows the id and the elapsed seconds; the seconds tick with
	// the wall clock, so the test reads them rather than spelling them.
	c.ChatCommand("stopwatch query foo:bar")
	got := servertest.FeedbackComponent(t, c)
	if got.Translate != "commands.stopwatch.query" {
		t.Fatalf("query answered %q, want the query key", got.Translate)
	}
	if len(got.With) != 2 || got.With[0].Text != "foo:bar" {
		t.Fatalf("query arguments = %+v, want the id first", got.With)
	}
	if seconds, err := strconv.ParseFloat(got.With[1].Text, 64); err != nil || seconds < 0 {
		t.Errorf("query seconds = %q, want a non-negative number", got.With[1].Text)
	}

	c.ChatCommand("stopwatch query other:id")
	if got := c.ExpectPlay(protocol.IDPlaySystemChat); !bytes.Equal(got, servertest.CommandErrorMessageForTest("commands.stopwatch.does_not_exist", "other:id")) {
		t.Errorf("querying a missing stopwatch answered % X, want the does-not-exist refusal", got)
	}

	c.ChatCommand("stopwatch restart foo:bar")
	if got := c.ExpectPlay(protocol.IDPlaySystemChat); !bytes.Equal(got, servertest.PlainFeedbackTextsForTest("commands.stopwatch.restart.success", "foo:bar")) {
		t.Errorf("restart answered % X, want the restarted notice", got)
	}

	c.ChatCommand("stopwatch restart other:id")
	if got := c.ExpectPlay(protocol.IDPlaySystemChat); !bytes.Equal(got, servertest.CommandErrorMessageForTest("commands.stopwatch.does_not_exist", "other:id")) {
		t.Errorf("restarting a missing stopwatch answered % X, want the does-not-exist refusal", got)
	}

	c.ChatCommand("stopwatch remove foo:bar")
	if got := c.ExpectPlay(protocol.IDPlaySystemChat); !bytes.Equal(got, servertest.PlainFeedbackTextsForTest("commands.stopwatch.remove.success", "foo:bar")) {
		t.Errorf("remove answered % X, want the removed notice", got)
	}

	c.ChatCommand("stopwatch remove foo:bar")
	if got := c.ExpectPlay(protocol.IDPlaySystemChat); !bytes.Equal(got, servertest.CommandErrorMessageForTest("commands.stopwatch.does_not_exist", "foo:bar")) {
		t.Errorf("removing a missing stopwatch answered % X, want the does-not-exist refusal", got)
	}

	c.Disconnect()
}
