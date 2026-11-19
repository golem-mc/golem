package commands

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// functionWorld starts a server whose world carries a data pack "test" with
// the given function files, keyed by their path under data/, and joins an
// operator.
func functionWorld(t *testing.T, files map[string]string) *servertest.Client {
	t.Helper()
	_, connect := startFunctionServer(t, files, "Testificate")
	c := connect().Join("Testificate")
	return c
}

// startFunctionServer starts a server whose world carries a data pack "test"
// with the given function files, the named players its operators.
func startFunctionServer(t *testing.T, files map[string]string, ops ...string) (*server.Server, func() *servertest.Client) {
	t.Helper()
	return startFunctionServerIn(t, t.TempDir(), files, ops...)
}

// startFunctionServerIn is startFunctionServer with dir as the server's
// directory, where /debug writes its traces.
func startFunctionServerIn(t *testing.T, dir string, files map[string]string, ops ...string) (*server.Server, func() *servertest.Client) {
	t.Helper()
	return startFunctionServerWith(t, dir, files, nil, ops...)
}

// startFunctionServerWith is startFunctionServerIn with configure, when not
// nil, applied to the config before the server starts.
func startFunctionServerWith(t *testing.T, dir string, files map[string]string, configure func(*config.Config), ops ...string) (*server.Server, func() *servertest.Client) {
	t.Helper()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.LevelName = t.TempDir()
	if configure != nil {
		configure(cfg)
	}
	pack := filepath.Join(cfg.LevelName, "datapacks", "test")
	writeFile(t, filepath.Join(pack, "pack.mcmeta"), `{"pack":{"pack_format":101,"description":"test"}}`)
	for name, body := range files {
		writeFile(t, filepath.Join(pack, "data", filepath.FromSlash(name)), body)
	}
	return servertest.Start(t, cfg, servertest.OpsListing(t, ops...), func(s *server.Server) {
		s.SetConfigFile(filepath.Join(dir, "server.properties"))
	})
}

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// expectFunctionResult reads the "Function %s returned %s" feedback.
func expectFunctionResult(c *servertest.Client, id string, value int64) {
	c.T.Helper()
	c.ExpectCommandFeedbackText("commands.function.result", protocol.Text{Text: id}, protocol.Text{Number: &value})
}

// TestFunctionRunsEveryLineSilently checks that /function loads a data pack
// function and runs each of its commands, comments and blank lines skipped
// and a trailing backslash joining a line to the next, with only the
// scheduled line heard: the commands' own feedback is suppressed.
func TestFunctionRunsEveryLineSilently(t *testing.T) {
	t.Parallel()
	c := functionWorld(t, map[string]string{
		"t/function/setup.mcfunction": "# a comment\r\n" +
			"data merge storage t:s {a:1}\r\n" +
			"\r\n" +
			"   data merge storage t:s \\\n" +
			"      {b:2}\n" +
			"scoreboard objectives add points dummy\n" +
			"list\n" +
			"data get storage t:s missing\n",
		"t/function/nested/deep.mcfunction": "data merge storage t:s {deep:1}\n",
		// A line that does not parse keeps the whole file out.
		"t/function/broken.mcfunction": "data merge storage t:s {broken:1}\nnot_a_command\n",
		// A leading slash is no command.
		"t/function/slashed.mcfunction": "/data merge storage t:s {slashed:1}\n",
	})

	c.ChatCommand("function t:setup")
	c.ExpectCommandFeedback("commands.function.scheduled.single", "t:setup")
	c.ChatCommand("data get storage t:s a")
	c.ExpectCommandFeedback("commands.data.storage.query", "t:s", "1")
	c.ChatCommand("data get storage t:s b")
	c.ExpectCommandFeedback("commands.data.storage.query", "t:s", "2")
	// The objective was added: a second add is refused.
	c.ChatCommand("scoreboard objectives add points dummy")
	c.ExpectCommandFailure(servertest.CommandErrorMessageForTest("commands.scoreboard.objectives.add.duplicate"))

	c.ChatCommand("function t:nested/deep")
	c.ExpectCommandFeedback("commands.function.scheduled.single", "t:nested/deep")
	c.ChatCommand("data get storage t:s deep")
	c.ExpectCommandFeedback("commands.data.storage.query", "t:s", "1")

	for _, id := range []string{"t:broken", "t:slashed", "t:missing"} {
		c.ChatCommand("function " + id)
		c.ExpectCommandFailure(servertest.CommandErrorMessageForTest("arguments.function.unknown", id))
	}
	c.ChatCommand("data get storage t:s broken")
	c.ExpectCommandFailure(servertest.CommandErrorMessageForTest("arguments.nbtpath.nothing_found", "broken"))

	// No function tags are loaded: a tag names no functions.
	c.ChatCommand("function #t:all")
	c.ExpectCommandFailure(servertest.CommandErrorMessageForTest("commands.function.scheduled.no_functions", "t:all"))

	// Tab completion offers the loaded functions.
	c.CommandSuggestion(1, "/function t:")
	_, _, _, matches := c.ExpectCommandSuggestions()
	if want := []string{"t:nested/deep", "t:setup"}; !slices.Equal(matches, want) {
		t.Errorf("function suggestions = %v, want %v", matches, want)
	}
	c.Disconnect()
}

// TestFunctionReturnEndsTheFunction checks /return: a value ends the
// function and reaches /function's result line and an execute store, a fail
// returns 0, a return run hands back its command's result, and a function
// called from another returns to its caller alone.
func TestFunctionReturnEndsTheFunction(t *testing.T) {
	t.Parallel()
	c := functionWorld(t, map[string]string{
		"t/function/ret.mcfunction":      "data merge storage t:s {before:1}\nreturn 7\ndata merge storage t:s {after:1}\n",
		"t/function/fail.mcfunction":     "return fail\n",
		"t/function/retrun.mcfunction":   "return run data get storage t:s before 5\ndata merge storage t:s {afterrun:1}\n",
		"t/function/outer.mcfunction":    "function t:ret\ndata merge storage t:s {outer:1}\n",
		"t/function/outerret.mcfunction": "return run function t:ret\ndata merge storage t:s {late:1}\n",
		"t/function/noreturn.mcfunction": "data merge storage t:s {plain:1}\n",
	})

	c.ChatCommand("function t:ret")
	c.ExpectCommandFeedback("commands.function.scheduled.single", "t:ret")
	expectFunctionResult(c, "t:ret", 7)
	c.ChatCommand("data get storage t:s after")
	c.ExpectCommandFailure(servertest.CommandErrorMessageForTest("arguments.nbtpath.nothing_found", "after"))

	c.ChatCommand("execute store result storage t:s r int 1 run function t:ret")
	c.ExpectCommandFeedback("commands.function.scheduled.single", "t:ret")
	expectFunctionResult(c, "t:ret", 7)
	c.ChatCommand("data get storage t:s r")
	c.ExpectCommandFeedback("commands.data.storage.query", "t:s", "7")

	c.ChatCommand("execute store success storage t:s ok int 1 run function t:fail")
	c.ExpectCommandFeedback("commands.function.scheduled.single", "t:fail")
	expectFunctionResult(c, "t:fail", 0)
	c.ChatCommand("data get storage t:s ok")
	c.ExpectCommandFeedback("commands.data.storage.query", "t:s", "0")

	c.ChatCommand("function t:retrun")
	c.ExpectCommandFeedback("commands.function.scheduled.single", "t:retrun")
	expectFunctionResult(c, "t:retrun", 5)
	c.ChatCommand("data get storage t:s afterrun")
	c.ExpectCommandFailure(servertest.CommandErrorMessageForTest("arguments.nbtpath.nothing_found", "afterrun"))

	// The inner call's lines say nothing, and its return ends only itself.
	c.ChatCommand("function t:outer")
	c.ExpectCommandFeedback("commands.function.scheduled.single", "t:outer")
	c.ChatCommand("data get storage t:s outer")
	c.ExpectCommandFeedback("commands.data.storage.query", "t:s", "1")

	c.ChatCommand("function t:outerret")
	c.ExpectCommandFeedback("commands.function.scheduled.single", "t:outerret")
	expectFunctionResult(c, "t:outerret", 7)
	c.ChatCommand("data get storage t:s late")
	c.ExpectCommandFailure(servertest.CommandErrorMessageForTest("arguments.nbtpath.nothing_found", "late"))

	// A function that never returns leaves a store untouched.
	c.ChatCommand("data merge storage t:s {kept:3}")
	c.ExpectCommandFeedback("commands.data.storage.modified", "t:s")
	c.ChatCommand("execute store result storage t:s kept int 1 run function t:noreturn")
	c.ExpectCommandFeedback("commands.function.scheduled.single", "t:noreturn")
	c.ChatCommand("data get storage t:s kept")
	c.ExpectCommandFeedback("commands.data.storage.query", "t:s", "3")
	c.Disconnect()
}

// TestFunctionStopsAtTheSequenceLimit checks that max_command_sequence_length
// caps every command a /function runs, the call itself counted, a return past
// the limit included, and stops a function that calls itself.
func TestFunctionStopsAtTheSequenceLimit(t *testing.T) {
	t.Parallel()
	c := functionWorld(t, map[string]string{
		"t/function/count.mcfunction": "data merge storage t:c {n1:1}\ndata merge storage t:c {n2:1}\n" +
			"data merge storage t:c {n3:1}\nreturn 9\n",
		"t/function/loop.mcfunction": "scoreboard players add n c 1\nfunction t:loop\n",
	})

	c.ChatCommand("gamerule max_command_sequence_length 3")
	c.ExpectCommandFeedback("commands.gamerule.set", "max_command_sequence_length", "3")
	c.ChatCommand("function t:count")
	c.ExpectCommandFeedback("commands.function.scheduled.single", "t:count")
	c.ChatCommand("data get storage t:c n2")
	c.ExpectCommandFeedback("commands.data.storage.query", "t:c", "1")
	c.ChatCommand("data get storage t:c n3")
	c.ExpectCommandFailure(servertest.CommandErrorMessageForTest("arguments.nbtpath.nothing_found", "n3"))

	// Each round of the loop costs its call and its add, the function
	// line itself nothing: 1000 commands are 500 rounds.
	c.ChatCommand("gamerule max_command_sequence_length 1000")
	c.ExpectCommandFeedback("commands.gamerule.set", "max_command_sequence_length", "1000")
	c.ChatCommand("scoreboard objectives add c dummy")
	expectScoreboardFeedback(t, c, "commands.scoreboard.objectives.add.success")
	c.ChatCommand("function t:loop")
	c.ExpectCommandFeedback("commands.function.scheduled.single", "t:loop")
	c.ChatCommand("scoreboard players get n c")
	if with := expectScoreboardFeedback(t, c, "commands.scoreboard.players.get.success"); len(with) != 3 || number(with[1]) != 500 {
		t.Errorf("rounds the loop ran = %+v, want 500", with)
	}
	c.Disconnect()
}

func TestExecuteIfFunction(t *testing.T) {
	t.Parallel()
	c := functionWorld(t, map[string]string{
		"t/function/yes.mcfunction":   "return 3\n",
		"t/function/zero.mcfunction":  "return 0\n",
		"t/function/plain.mcfunction": "data merge storage t:s {plain:1}\n",
	})
	for _, tc := range []struct {
		test, key string
		ran       bool
	}{
		{"if function t:yes", "a", true},
		{"if function t:zero", "b", false},
		{"unless function t:zero", "c", true},
		{"unless function t:yes", "d", false},
		{"if function t:plain", "e", false},
		{"unless function t:plain", "f", true},
	} {
		c.ChatCommand("execute " + tc.test + " run data merge storage t:s {" + tc.key + ":1}")
		if tc.ran {
			c.ExpectCommandFeedback("commands.data.storage.modified", "t:s")
		}
	}
	for _, tc := range []struct {
		key string
		ran bool
	}{{"a", true}, {"b", false}, {"c", true}, {"d", false}, {"e", false}, {"f", true}, {"plain", true}} {
		c.ChatCommand("data get storage t:s " + tc.key)
		if tc.ran {
			c.ExpectCommandFeedback("commands.data.storage.query", "t:s", "1")
		} else {
			c.ExpectCommandFailure(servertest.CommandErrorMessageForTest("arguments.nbtpath.nothing_found", tc.key))
		}
	}
}

// TestDebugFunctionWritesTheTraceFile checks that /debug function runs the
// function, reports its command count and file, and writes the trace.
func TestDebugFunctionWritesTheTraceFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	_, connect := startFunctionServerIn(t, dir, map[string]string{
		"t/function/f.mcfunction": "data merge storage t:s {a:1}\nfunction t:g\n",
		"t/function/g.mcfunction": "data get storage t:s a\n",
	}, "Testificate")
	c := connect().Join("Testificate")

	c.ChatCommand("debug function t:f")
	m := servertest.FeedbackComponent(t, c)
	if m.Translate != "commands.debug.function.success.single" || len(m.With) != 3 || *m.With[0].Number != 2 || m.With[1].Text != "t:f" {
		t.Fatalf("feedback = %+v, want 2 commands of t:f", m)
	}
	name := m.With[2].Text
	body, err := os.ReadFile(filepath.Join(dir, "debug", name))
	if err != nil {
		t.Fatal(err)
	}
	want := "t:f\n" +
		"    [F] t:f size=2\n" +
		"        [C] data merge storage t:s {a:1} -> 1\n" +
		"        [C] function t:g\n" +
		"        [F] t:g size=1\n" +
		"            [C] data get storage t:s a -> 1\n"
	if string(body) != want {
		t.Errorf("trace =\n%s\nwant\n%s", body, want)
	}
}
