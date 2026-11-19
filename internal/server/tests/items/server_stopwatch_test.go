package items

import (
	"bytes"
	"github.com/golem-mc/golem/internal/server/servertest"
	"path/filepath"
	"slices"
	"strconv"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/nbt"
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

// TestExecuteIfStopwatch checks the /execute stopwatch test against the
// elapsed seconds a created stopwatch carries: a range it is inside passes,
// one it is outside fails, and an id that names no stopwatch refuses the way
// an empty registry does.
func TestExecuteIfStopwatch(t *testing.T) {
	t.Parallel()
	_, c := servertest.StartExecuteWorld(t, "Testificate")

	c.ChatCommand("stopwatch create foo:bar")
	c.ExpectPlay(protocol.IDPlaySystemChat)

	c.ChatCommand("execute if stopwatch foo:bar 0..")
	if got := c.ExpectPlay(protocol.IDPlaySystemChat); !bytes.Equal(got, servertest.PlainFeedbackTextsForTest("commands.execute.conditional.pass")) {
		t.Errorf("a fresh stopwatch in 0.. answered % X, want the conditional pass", got)
	}

	// A fresh stopwatch has run for a fraction of a second, so an upper
	// bound of a million seconds passes and a lower bound of one fails.
	c.ChatCommand("execute if stopwatch foo:bar ..1000000")
	if got := c.ExpectPlay(protocol.IDPlaySystemChat); !bytes.Equal(got, servertest.PlainFeedbackTextsForTest("commands.execute.conditional.pass")) {
		t.Errorf("a fresh stopwatch in ..1000000 answered % X, want the conditional pass", got)
	}
	c.ChatCommand("execute if stopwatch foo:bar 1000000..")
	if got := c.ExpectPlay(protocol.IDPlaySystemChat); !bytes.Equal(got, servertest.CommandErrorMessageForTest("commands.execute.conditional.fail")) {
		t.Errorf("a fresh stopwatch in 1000000.. answered % X, want the conditional failure", got)
	}

	c.ChatCommand("execute if stopwatch other:id 0..")
	if got := c.ExpectPlay(protocol.IDPlaySystemChat); !bytes.Equal(got, servertest.CommandErrorMessageForTest("commands.stopwatch.does_not_exist", "other:id")) {
		t.Errorf("a missing stopwatch answered % X, want the does-not-exist refusal", got)
	}

	c.Disconnect()
}

// TestExecuteIfStopwatchSuggestsIDs checks the id argument of an
// /execute if stopwatch test offers the stopwatches that exist.
func TestExecuteIfStopwatchSuggestsIDs(t *testing.T) {
	t.Parallel()
	_, c := servertest.StartExecuteWorld(t, "Testificate")

	c.ChatCommand("stopwatch create foo:bar")
	c.ExpectPlay(protocol.IDPlaySystemChat)

	c.CommandSuggestion(1, "/execute if stopwatch ")
	_, _, _, matches := c.ExpectCommandSuggestions()
	if !slices.Contains(matches, "foo:bar") {
		t.Errorf("suggestions %v do not carry foo:bar", matches)
	}

	c.Disconnect()
}

// TestStopwatchSurvivesARestart checks that a stopwatch outlives the process:
// the create writes data/minecraft/stopwatches.dat, and a second server
// reading the same directory finds the stopwatch the first one kept.
func TestStopwatchSurvivesARestart(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.LevelName = t.TempDir()

	srv, c := servertest.Join(t, cfg, "Testificate", servertest.OpsListing(t, "Testificate"))
	c.ChatCommand("stopwatch create foo:bar")
	if got := c.ExpectPlay(protocol.IDPlaySystemChat); !bytes.Equal(got, servertest.PlainFeedbackTextsForTest("commands.stopwatch.create.success", "foo:bar")) {
		t.Errorf("create answered % X, want the created notice", got)
	}
	c.Disconnect()
	srv.Shutdown()

	// The file lives under the world's data/minecraft, holding the id and
	// the milliseconds it had run by the save.
	data := servertest.ReadSavedFile(t, filepath.Join(cfg.LevelName, "data", "minecraft", "stopwatches.dat"), "data")
	stopwatches, ok := nbt.Get[nbt.Compound](data, "stopwatches")
	if !ok {
		t.Fatal("stopwatches.dat has no stopwatches compound")
	}
	if _, ok := nbt.Get[nbt.Long](stopwatches, "foo:bar"); !ok {
		t.Errorf("stopwatches.dat holds %v, want the foo:bar long", stopwatches)
	}

	// A server of its own, as after a restart, pointed at the same world.
	restarted := *cfg
	_, again := servertest.Join(t, &restarted, "Testificate", servertest.OpsListing(t, "Testificate"))
	again.ChatCommand("execute if stopwatch foo:bar 0..")
	if got := again.ExpectPlay(protocol.IDPlaySystemChat); !bytes.Equal(got, servertest.PlainFeedbackTextsForTest("commands.execute.conditional.pass")) {
		t.Errorf("the restarted server answered % X, want the stopwatch kept", got)
	}
	again.Disconnect()
}
