package commands

import (
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"slices"
	"strings"
	"testing"
	"time"

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

// TestFunctionStopsBeforeFreeEntriesAtZeroQuota checks that once the quota
// is spent the run stops before a return that costs nothing.
// vanilla: ExecutionContext.runCommandQueue
func TestFunctionStopsBeforeFreeEntriesAtZeroQuota(t *testing.T) {
	t.Parallel()
	c := functionWorld(t, map[string]string{
		"t/function/top.mcfunction": "function t:n\nreturn 5\n",
		"t/function/n.mcfunction":   "return 1\n",
	})
	c.ChatCommand("gamerule max_command_sequence_length 4")
	c.ExpectCommandFeedback("commands.gamerule.set", "max_command_sequence_length", "4")
	c.ChatCommand("execute store result storage t:c r int 1 run function t:top")
	c.ExpectCommandFeedback("commands.function.scheduled.single", "t:top")
	c.ChatCommand("data get storage t:c r")
	c.ExpectCommandFailure(servertest.CommandErrorMessageForTest("arguments.nbtpath.nothing_found", "r"))
	c.Disconnect()
}

// TestFunctionCallsDoNotGrowTheStack checks that a function calling itself,
// plainly or under a return run, runs to the sequence limit without
// overflowing the Go stack, which a recursive call would at this limit.
// The stack cap is lowered so a short run proves it, not a run that takes as
// long as the read deadline.
func TestFunctionCallsDoNotGrowTheStack(t *testing.T) {
	t.Parallel()
	old := debug.SetMaxStack(8 << 20)
	t.Cleanup(func() { debug.SetMaxStack(old) })
	c := functionWorld(t, map[string]string{
		"t/function/deep.mcfunction": "function t:deep\n",
		"t/function/ret.mcfunction":  "return run function t:ret\n",
	})

	c.ChatCommand("gamerule max_command_sequence_length 200000")
	c.ExpectCommandFeedback("commands.gamerule.set", "max_command_sequence_length", "200000")
	c.ChatCommand("function t:deep")
	c.ExpectCommandFeedback("commands.function.scheduled.single", "t:deep")
	c.ChatCommand("function t:ret")
	c.ExpectCommandFeedback("commands.function.scheduled.single", "t:ret")
	c.Disconnect()
}

// TestFunctionReturnRunThroughExecute checks a return run whose command is an
// execute: a plain command or a closing conditional runs in the first context
// alone, its result the function's, and a chain left with no context fails
// the function, which ends either way.
func TestFunctionReturnRunThroughExecute(t *testing.T) {
	t.Parallel()
	c := functionWorld(t, map[string]string{
		"t/function/each.mcfunction": "return run execute as @e[type=minecraft:cow] run data get storage t:s v 3\n" +
			"data merge storage t:s {after:1}\n",
		"t/function/cond.mcfunction": "return run execute as @e[type=minecraft:cow] if entity @s\n",
		"t/function/none.mcfunction": "return run execute as @e[type=minecraft:pig] run data get storage t:s v\n" +
			"data merge storage t:s {after:1}\n",
		"t/function/bump.mcfunction":    "return run execute as @e[type=minecraft:cow] run scoreboard players add #n c 1\n",
		"t/function/bumpret.mcfunction": "scoreboard players add #m c 1\nreturn 4\n",
		"t/function/eachfn.mcfunction":  "return run execute as @e[type=minecraft:cow] run function t:bumpret\n",
		"t/function/plain.mcfunction":   "scoreboard players add #p c 1\n",
		"t/function/fallthrough.mcfunction": "return run execute as @e[type=minecraft:cow] run function t:plain\n" +
			"data merge storage t:s {after:1}\n",
	})
	c.ChatCommand("scoreboard objectives add c dummy")
	expectScoreboardFeedback(t, c, "commands.scoreboard.objectives.add.success")
	c.ChatCommand("data merge storage t:s {v:2}")
	c.ExpectCommandFeedback("commands.data.storage.modified", "t:s")
	for range 2 {
		c.ChatCommand("summon minecraft:cow 2.5 64 2.5")
		cow := c.ExpectMobSpawn(protocol.EntityTypeCow)
		c.ExpectCommandFeedbackText("commands.summon.success",
			servertest.EntityDisplayComponent("minecraft:cow", "entity.minecraft.cow", cow.Uuid))
	}

	for _, tc := range []struct {
		id    string
		value int64
	}{{"t:each", 6}, {"t:cond", 1}, {"t:none", 0}, {"t:bump", 1}, {"t:eachfn", 4}, {"t:fallthrough", 0}} {
		c.ChatCommand("function " + tc.id)
		c.ExpectCommandFeedback("commands.function.scheduled.single", tc.id)
		expectFunctionResult(c, tc.id, tc.value)
	}
	c.ChatCommand("data get storage t:s after")
	c.ExpectCommandFailure(servertest.CommandErrorMessageForTest("arguments.nbtpath.nothing_found", "after"))
	// Each of the three ran for the first cow alone: the plain command by
	// the return run's own rule, the second call dropped by the first's
	// return, and by the first's failing to return.
	for _, holder := range []string{"#n", "#m", "#p"} {
		c.ChatCommand("scoreboard players get " + holder + " c")
		if with := expectScoreboardFeedback(t, c, "commands.scoreboard.players.get.success"); len(with) != 3 || number(with[1]) != 1 {
			t.Errorf("%s = %+v, want 1", holder, with)
		}
	}
	c.Disconnect()
}

// TestExecuteIfFunctionInChains checks a function test over a tag, whose
// first return decides; an unknown name, refused; a test failing under a
// return run, which fails the function; and a chain walking on past the test
// over every context that passed.
func TestExecuteIfFunctionInChains(t *testing.T) {
	t.Parallel()
	c := functionWorld(t, map[string]string{
		"t/function/yes.mcfunction":      "return 3\n",
		"t/function/zero.mcfunction":     "return 0\n",
		"t/tags/function/zerofirst.json": `{"values":["t:zero","t:yes"]}`,
		"t/function/failing.mcfunction": "return run execute if function t:zero run data merge storage t:s {ran:1}\n" +
			"data merge storage t:s {after:1}\n",
	})
	c.ChatCommand("scoreboard objectives add c dummy")
	expectScoreboardFeedback(t, c, "commands.scoreboard.objectives.add.success")
	for range 2 {
		c.ChatCommand("summon minecraft:cow 2.5 64 2.5")
		cow := c.ExpectMobSpawn(protocol.EntityTypeCow)
		c.ExpectCommandFeedbackText("commands.summon.success",
			servertest.EntityDisplayComponent("minecraft:cow", "entity.minecraft.cow", cow.Uuid))
	}

	c.ChatCommand("execute if function #t:zerofirst run data merge storage t:s {tagif:1}")
	c.ChatCommand("execute unless function #t:zerofirst run data merge storage t:s {tagunless:1}")
	c.ExpectCommandFeedback("commands.data.storage.modified", "t:s")

	c.ChatCommand("execute if function t:missing run data merge storage t:s {missing:1}")
	c.ExpectCommandFailure(servertest.CommandErrorMessageForTest("arguments.function.unknown", "t:missing"))

	c.ChatCommand("function t:failing")
	c.ExpectCommandFeedback("commands.function.scheduled.single", "t:failing")
	expectFunctionResult(c, "t:failing", 0)

	c.ChatCommand("execute as @e[type=minecraft:cow] if function t:yes run scoreboard players add #before c 1")
	expectScoreboardFeedback(t, c, "commands.scoreboard.players.add.success.single")
	expectScoreboardFeedback(t, c, "commands.scoreboard.players.add.success.single")
	c.ChatCommand("execute if function t:yes as @e[type=minecraft:cow] run scoreboard players add #after c 1")
	expectScoreboardFeedback(t, c, "commands.scoreboard.players.add.success.single")
	expectScoreboardFeedback(t, c, "commands.scoreboard.players.add.success.single")

	for _, tc := range []struct {
		key string
		ran bool
	}{{"tagif", false}, {"tagunless", true}, {"missing", false}, {"ran", false}, {"after", false}} {
		c.ChatCommand("data get storage t:s " + tc.key)
		if tc.ran {
			c.ExpectCommandFeedback("commands.data.storage.query", "t:s", "1")
		} else {
			c.ExpectCommandFailure(servertest.CommandErrorMessageForTest("arguments.nbtpath.nothing_found", tc.key))
		}
	}
	for _, holder := range []string{"#before", "#after"} {
		c.ChatCommand("scoreboard players get " + holder + " c")
		if with := expectScoreboardFeedback(t, c, "commands.scoreboard.players.get.success"); len(with) != 3 || number(with[1]) != 2 {
			t.Errorf("%s = %+v, want 2", holder, with)
		}
	}
	c.Disconnect()
}

// TestFunctionTellsOtherOperatorsOnlyItsSchedule checks what a second
// operator reads of a /function: the scheduled line, and nothing of the
// commands the function ran.
func TestFunctionTellsOtherOperatorsOnlyItsSchedule(t *testing.T) {
	t.Parallel()
	srv, connect := startFunctionServer(t, map[string]string{
		"t/function/give.mcfunction": "give @s stone\n",
	}, "Alice", "Bob")
	alice := servertest.JoinFeedbackWorld(t, connect, "Alice", nil)
	bob := servertest.JoinFeedbackWorld(t, connect, "Bob", []*servertest.Client{alice})

	alice.ChatCommand("function t:give")
	alice.ExpectCommandFeedback("commands.function.scheduled.single", "t:give")
	alice.Settle()
	servertest.AwaitAdminBroadcast(t, bob, servertest.PlayerDisplayComponent("Alice"),
		protocol.Text{Translate: "commands.function.scheduled.single", With: []protocol.Text{{Text: "t:give"}}})
	bob.ExpectNoneWithinTicks(srv, 1, servertest.PacketIs(protocol.IDPlaySystemChat))
	alice.Disconnect()
	bob.Disconnect()
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

// TestDebugStopWithoutStartRefuses checks the profiler's two refusals.
func TestDebugStopWithoutStartRefuses(t *testing.T) {
	t.Parallel()
	c := functionWorld(t, nil)
	c.ChatCommand("debug stop")
	c.ExpectCommandFailure(servertest.CommandErrorMessageForTest("commands.debug.notRunning"))
	c.ChatCommand("debug start")
	c.ExpectCommandFeedback("commands.debug.started")
	c.ChatCommand("debug start")
	c.ExpectCommandFailure(servertest.CommandErrorMessageForTest("commands.debug.alreadyRunning"))
}

// TestDebugStopCountsTicks checks that start, 20 ticks and stop report 20
// ticks.
func TestDebugStopCountsTicks(t *testing.T) {
	t.Parallel()
	srv, connect := startFunctionServer(t, nil, "Testificate")
	c := connect().Join("Testificate")
	servertest.HoldTicks(t, srv)
	c.ChatCommand("debug start")
	c.ExpectCommandFeedback("commands.debug.started")
	before := srv.RecordedTicks()
	servertest.RunTicks(t, srv, 20)
	// The tick-end signal precedes the recording of that tick.
	deadline := time.Now().Add(10 * time.Second)
	for srv.RecordedTicks() < before+20 {
		if time.Now().After(deadline) {
			t.Fatalf("recorded ticks = %d, want %d", srv.RecordedTicks(), before+20)
		}
		runtime.Gosched()
	}
	c.ChatCommand("debug stop")
	m := servertest.FeedbackComponent(t, c)
	if m.Translate != "commands.debug.stopped" || len(m.With) != 3 {
		t.Fatalf("feedback = %+v, want commands.debug.stopped", m)
	}
	if got := *m.With[1].Number; got != 20 {
		t.Errorf("ticks = %d, want 20", got)
	}
}

// TestDebugFunctionTracesErrors checks the [E] line of a failing command and
// that the next line does not take a result for it.
func TestDebugFunctionTracesErrors(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	_, connect := startFunctionServerIn(t, dir, map[string]string{
		"t/function/f.mcfunction": "function t:missing\ndata get storage t:s a\n",
	}, "Testificate")
	c := connect().Join("Testificate")

	c.ChatCommand("debug function t:f")
	m := servertest.FeedbackComponent(t, c)
	body, err := os.ReadFile(filepath.Join(dir, "debug", m.With[2].Text))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(string(body), "\n")
	if len(lines) < 5 || !strings.HasPrefix(lines[3], "            [E] ") ||
		!strings.HasPrefix(lines[4], "        [C] data get storage t:s a") {
		t.Errorf("trace =\n%s", body)
	}
}

// TestFunctionsCompileAtTheConfiguredPermissionLevel checks that data pack
// functions are matched against function-permission-level's tree: a line
// above it keeps its file out of the library, one at or below it loads.
// vanilla: ServerFunctionLibrary.reload, Commands.createCompilationContext
func TestFunctionsCompileAtTheConfiguredPermissionLevel(t *testing.T) {
	t.Parallel()
	files := map[string]string{
		// setidletimeout is level 3, data level 2.
		"t/function/admin.mcfunction": "data merge storage t:s {ran:1}\nsetidletimeout 7\n",
		"t/function/plain.mcfunction": "data merge storage t:s {plain:1}\n",
	}
	join := func(t *testing.T, configure func(*config.Config)) *servertest.Client {
		t.Helper()
		_, connect := startFunctionServerWith(t, t.TempDir(), files, configure, "Testificate")
		return connect().Join("Testificate")
	}
	refused := func(c *servertest.Client, id string) {
		c.T.Helper()
		c.ChatCommand("function " + id)
		c.ExpectCommandFailure(servertest.CommandErrorMessageForTest("arguments.function.unknown", id))
	}
	runs := func(c *servertest.Client, id, key string) {
		c.T.Helper()
		c.ChatCommand("function " + id)
		c.ExpectCommandFeedback("commands.function.scheduled.single", id)
		c.ChatCommand("data get storage t:s " + key)
		c.ExpectCommandFeedback("commands.data.storage.query", "t:s", "1")
	}

	t.Run("level 3", func(t *testing.T) {
		t.Parallel()
		c := join(t, func(cfg *config.Config) { cfg.FunctionPermissionLevel = 3 })
		runs(c, "t:admin", "ran")
	})
	t.Run("default", func(t *testing.T) {
		t.Parallel()
		c := join(t, nil)
		refused(c, "t:admin")
		runs(c, "t:plain", "plain")
	})
	t.Run("level 1", func(t *testing.T) {
		t.Parallel()
		c := join(t, func(cfg *config.Config) { cfg.FunctionPermissionLevel = 1 })
		refused(c, "t:plain")
	})
}
