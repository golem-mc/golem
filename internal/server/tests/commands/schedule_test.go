package commands

import (
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/nbt"
)

// scheduleFiles are the pack files the schedule tests run: two counters, and
// tags over them.
var scheduleFiles = map[string]string{
	"minecraft/function/schedule_probe.mcfunction": "scoreboard players add #a cnt 1\n",
	"t/function/count.mcfunction":                  "scoreboard players add #a cnt 1\n",
	"t/function/count2.mcfunction":                 "scoreboard players add #b cnt 1\n",
	// first and second leave #o at 6 only when first runs before second.
	"t/function/first.mcfunction":  "scoreboard players set #o cnt 5\n",
	"t/function/second.mcfunction": "scoreboard players add #o cnt 1\n",
	// A level 3 command keeps the whole function from loading.
	"t/function/grant.mcfunction": "op Testificate\n",
	// An optional entry that is missing is skipped.
	"t/tags/function/all.json": `{"values":["t:count",{"id":"t:count2","required":true},` +
		`{"id":"t:nope","required":false}]}`,
	"t/tags/function/outer.json": `{"values":["#t:all","t:count"]}`,
	// A required entry that is missing keeps the tag out.
	"t/tags/function/broken.json": `{"values":["t:nope"]}`,
}

// scheduleWorld starts a server with the schedule pack, its loop held, and
// joins an operator with the counters at zero.
func scheduleWorld(t *testing.T) (*server.Server, *servertest.Client, *config.Config) {
	t.Helper()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.LevelName = t.TempDir()
	return startScheduleWorld(t, cfg, true)
}

// startScheduleWorld starts a server on cfg's world; fresh sets up the
// counters.
func startScheduleWorld(t *testing.T, cfg *config.Config, fresh bool) (*server.Server, *servertest.Client, *config.Config) {
	t.Helper()
	pack := filepath.Join(cfg.LevelName, "datapacks", "test")
	writeFile(t, filepath.Join(pack, "pack.mcmeta"), `{"pack":{"pack_format":101,"description":"test"}}`)
	for name, body := range scheduleFiles {
		writeFile(t, filepath.Join(pack, "data", filepath.FromSlash(name)), body)
	}
	srv, connect := servertest.Start(t, cfg, servertest.OpsListing(t, "Testificate"))
	// The world's age, which the events are due by, stands still through
	// the join.
	servertest.FreezeUntilHeld(t, srv)
	c := connect().Join("Testificate")
	if fresh {
		c.ChatCommand("scoreboard objectives add cnt dummy")
		servertest.FeedbackComponent(t, c)
		c.ChatCommand("scoreboard players set #a cnt 0")
		servertest.FeedbackComponent(t, c)
		c.ChatCommand("scoreboard players set #b cnt 0")
		servertest.FeedbackComponent(t, c)
		c.ChatCommand("scoreboard players set #o cnt 0")
		servertest.FeedbackComponent(t, c)
	}
	servertest.HoldTicks(t, srv)
	return srv, c, cfg
}

// score reads a counter.
func score(t *testing.T, c *servertest.Client, holder string) int64 {
	t.Helper()
	c.ChatCommand("scoreboard players get " + holder + " cnt")
	with := expectScoreboardFeedback(t, c, "commands.scoreboard.players.get.success")
	return number(with[1])
}

// schedule runs a /schedule and reads the creation line, returning the
// tick it was queued for.
func schedule(t *testing.T, c *servertest.Client, args, key, id string, ticks int64) {
	t.Helper()
	c.ChatCommand("schedule function " + args)
	with := expectScoreboardFeedback(t, c, key)
	if len(with) != 3 || with[0].Text != id || number(with[1]) != ticks {
		t.Errorf("/schedule function %s said %+v, want %s in %d ticks", args, with, id, ticks)
	}
}

// TestScheduleRunsOnTheDueTick checks that a scheduled function runs on the
// tick the game time reaches, not before, with the time argument's units,
// and that it runs once.
// vanilla: ScheduleCommand.schedule, TimerQueue.tick
func TestScheduleRunsOnTheDueTick(t *testing.T) {
	t.Parallel()
	srv, c, _ := scheduleWorld(t)

	schedule(t, c, "t:count 5", "commands.schedule.created.function", "t:count", 5)
	servertest.RunTicks(t, srv, 4)
	if got := score(t, c, "#a"); got != 0 {
		t.Fatalf("count after 4 of 5 ticks = %d, want 0", got)
	}
	servertest.RunTicks(t, srv, 1)
	if got := score(t, c, "#a"); got != 1 {
		t.Fatalf("count on the 5th tick = %d, want 1", got)
	}
	servertest.RunTicks(t, srv, 30)
	if got := score(t, c, "#a"); got != 1 {
		t.Errorf("count 30 ticks on = %d, want it run once", got)
	}

	// 1s is 20 ticks.
	schedule(t, c, "t:count 1s", "commands.schedule.created.function", "t:count", 20)
	servertest.RunTicks(t, srv, 19)
	if got := score(t, c, "#a"); got != 1 {
		t.Fatalf("count at 19 of 20 ticks = %d, want 1", got)
	}
	servertest.RunTicks(t, srv, 1)
	if got := score(t, c, "#a"); got != 2 {
		t.Errorf("count at 20 of 20 ticks = %d, want 2", got)
	}

	// 1d is 24000 ticks; running them would outlast the keep-alive, so the
	// queued tick count is checked.
	schedule(t, c, "t:count 1d", "commands.schedule.created.function", "t:count", 24000)
	c.Disconnect()
}

// TestScheduleSameTickOrder checks that events due on one tick run in the
// order they were scheduled, whichever function each names.
// vanilla: TimerQueue.schedule, TimerQueue.tick
func TestScheduleSameTickOrder(t *testing.T) {
	t.Parallel()
	srv, c, _ := scheduleWorld(t)

	c.ChatCommand("schedule function t:first 4")
	servertest.FeedbackComponent(t, c)
	c.ChatCommand("schedule function t:second 4")
	servertest.FeedbackComponent(t, c)
	servertest.RunTicks(t, srv, 4)
	if got := score(t, c, "#o"); got != 6 {
		t.Errorf("#o after first then second = %d, want 6", got)
	}

	c.ChatCommand("schedule function t:second 4")
	servertest.FeedbackComponent(t, c)
	c.ChatCommand("schedule function t:first 4")
	servertest.FeedbackComponent(t, c)
	servertest.RunTicks(t, srv, 4)
	if got := score(t, c, "#o"); got != 5 {
		t.Errorf("#o after second then first = %d, want 5", got)
	}
	c.Disconnect()
}

// TestScheduledFunctionRunsAtGamemasterLevel checks that scheduled functions
// run as the game loop's gamemaster-level source, and that a function with a
// level 3 command is not loaded, so it cannot be scheduled.
// vanilla: ServerFunctionManager.getGameLoopSender, ServerFunctionLibrary.reload
func TestScheduledFunctionRunsAtGamemasterLevel(t *testing.T) {
	t.Parallel()
	srv, c, _ := scheduleWorld(t)

	if got := srv.GameLoopPermLevel(); got != 2 {
		t.Errorf("scheduled functions run at level %d, want 2", got)
	}
	c.ChatCommand("schedule function t:grant 5")
	c.ExpectCommandFailure(servertest.CommandErrorMessageForTest("arguments.function.unknown", "t:grant"))
	c.Disconnect()
}

// TestScheduleReplaceAndAppend checks that replace, the default, drops the
// queued events of the function, append keeps them, and an append at a
// queued time adds nothing.
// vanilla: ScheduleCommand.schedule, TimerQueue.schedule
func TestScheduleReplaceAndAppend(t *testing.T) {
	t.Parallel()
	srv, c, _ := scheduleWorld(t)

	c.ChatCommand("schedule function t:count 10")
	servertest.FeedbackComponent(t, c)
	c.ChatCommand("schedule function t:count 3")
	servertest.FeedbackComponent(t, c)
	servertest.RunTicks(t, srv, 12)
	if got := score(t, c, "#a"); got != 1 {
		t.Errorf("count after two replacing schedules = %d, want 1", got)
	}

	c.ChatCommand("schedule function t:count 3 append")
	servertest.FeedbackComponent(t, c)
	c.ChatCommand("schedule function t:count 5 append")
	servertest.FeedbackComponent(t, c)
	c.ChatCommand("schedule function t:count 5 append")
	servertest.FeedbackComponent(t, c)
	servertest.RunTicks(t, srv, 8)
	if got := score(t, c, "#a"); got != 3 {
		t.Errorf("count after two appended events = %d, want 3 (the repeat at one time is dropped)", got)
	}

	c.ChatCommand("schedule function t:count 4 append")
	servertest.FeedbackComponent(t, c)
	c.ChatCommand("schedule function t:count 6 replace")
	servertest.FeedbackComponent(t, c)
	servertest.RunTicks(t, srv, 10)
	if got := score(t, c, "#a"); got != 4 {
		t.Errorf("count after an explicit replace = %d, want 4", got)
	}
}

// TestScheduleClear checks /schedule clear: it removes the events of the
// name, reports how many, and fails once none are left.
// vanilla: ScheduleCommand.remove
func TestScheduleClear(t *testing.T) {
	t.Parallel()
	srv, c, _ := scheduleWorld(t)

	c.ChatCommand("schedule function t:count 5 append")
	servertest.FeedbackComponent(t, c)
	c.ChatCommand("schedule function t:count 7 append")
	servertest.FeedbackComponent(t, c)
	c.ChatCommand("schedule function #t:all 5")
	servertest.FeedbackComponent(t, c)

	c.CommandSuggestion(1, "/schedule clear ")
	if _, _, _, matches := c.ExpectCommandSuggestions(); !slices.Equal(matches, []string{"#t:all", "t:count"}) {
		t.Errorf("clear suggestions = %v, want the queued names", matches)
	}

	c.ChatCommand("schedule clear t:count")
	with := expectScoreboardFeedback(t, c, "commands.schedule.cleared.success")
	if len(with) != 2 || number(with[0]) != 2 || with[1].Text != "t:count" {
		t.Errorf("clear said %+v, want 2 events of t:count", with)
	}
	c.ChatCommand("schedule clear t:count")
	if with := expectScoreboardRefusal(t, c, "commands.schedule.cleared.failure"); len(with) != 1 || with[0].Text != "t:count" {
		t.Errorf("second clear refusal args %+v, want t:count", with)
	}
	servertest.RunTicks(t, srv, 10)
	if got := score(t, c, "#a"); got != 1 {
		t.Errorf("count after clearing = %d, want only the tag's run", got)
	}

	c.ChatCommand("schedule clear #t:all")
	c.ExpectCommandError(servertest.CommandErrorMessageForTest("command.expected.separator"),
		servertest.CommandErrorContextForTest("schedule clear #t:all", len("schedule clear ")))
	c.Disconnect()
}

// vanilla: ScheduleCommand.register, ScheduleCommand.remove
func TestScheduleClearIdentifier(t *testing.T) {
	t.Parallel()
	srv, c, _ := scheduleWorld(t)
	schedule(t, c, "schedule_probe 100t", "commands.schedule.created.function", "minecraft:schedule_probe", 100)
	c.ChatCommand("schedule clear schedule_probe")
	with := expectScoreboardFeedback(t, c, "commands.schedule.cleared.success")
	if len(with) != 2 || number(with[0]) != 1 || with[1].Text != "minecraft:schedule_probe" {
		t.Errorf("clear said %+v, want 1 event of minecraft:schedule_probe", with)
	}
	c.ChatCommand("schedule clear foo")
	if with := expectScoreboardRefusal(t, c, "commands.schedule.cleared.failure"); len(with) != 1 || with[0].Text != "minecraft:foo" {
		t.Errorf("unknown clear said %+v, want minecraft:foo", with)
	}
	for _, id := range []string{"#t:all", "a/b:c"} {
		key := "command.expected.separator"
		if id == "a/b:c" {
			key = "argument.id.invalid"
		}
		c.ChatCommand("schedule clear " + id)
		c.ExpectCommandError(servertest.CommandErrorMessageForTest(key),
			servertest.CommandErrorContextForTest("schedule clear "+id, len("schedule clear ")))
	}
	servertest.RunTicks(t, srv, 100)
	if got := score(t, c, "#a"); got != 0 {
		t.Errorf("cleared function ran %d times, want 0", got)
	}
	c.Disconnect()
}

func TestScheduleClearIdentifierTree(t *testing.T) {
	t.Parallel()
	_, c := servertest.Dial(t, servertest.BlockTestConfig(), servertest.OpsListing(t, "Tester"))
	c.Login("Tester")
	c.Configure()
	nodes := servertest.DecodeCommands(t, c.AwaitPlay(protocol.IDPlayCommands))
	c.SkipJoin()
	node := nodes[0]
	for _, name := range []string{"schedule", "clear", "function"} {
		found := false
		for _, i := range node.Children {
			if nodes[i].Name == name {
				node, found = nodes[i], true
				break
			}
		}
		if !found {
			t.Fatalf("missing schedule clear node %s", name)
		}
	}
	if node.Parser != 36 || !node.Executable { // minecraft:resource_location
		t.Errorf("clear function = %+v, want executable identifier", node)
	}
	c.Disconnect()
}

// TestScheduleTag checks that a scheduled tag runs every function it holds
// in order, nested tags spliced in and repeats dropped, and that a tag that
// is not loaded schedules and runs nothing.
// vanilla: FunctionTagCallback.handle, TagLoader.build
func TestScheduleTag(t *testing.T) {
	t.Parallel()
	srv, c, _ := scheduleWorld(t)

	schedule(t, c, "#t:outer 2", "commands.schedule.created.tag", "t:outer", 2)
	schedule(t, c, "#t:broken 2", "commands.schedule.created.tag", "t:broken", 2)
	schedule(t, c, "#t:unknown 2", "commands.schedule.created.tag", "t:unknown", 2)
	servertest.RunTicks(t, srv, 2)
	// outer = all (count, count2) + count, a repeat.
	if a, b := score(t, c, "#a"), score(t, c, "#b"); a != 1 || b != 1 {
		t.Errorf("counters after the tag = %d, %d, want 1, 1", a, b)
	}

	c.ChatCommand("function #t:all")
	with := expectScoreboardFeedback(t, c, "commands.function.scheduled.multiple")
	if len(with) != 1 || len(with[0].Extra) != 3 || with[0].Extra[0].Text != "t:count" || with[0].Extra[2].Text != "t:count2" {
		t.Errorf("/function #t:all announced %+v, want t:count and t:count2", with)
	}
	if a, b := score(t, c, "#a"), score(t, c, "#b"); a != 2 || b != 2 {
		t.Errorf("counters after /function #t:all = %d, %d, want 2, 2", a, b)
	}
	c.ChatCommand("function #t:broken")
	c.ExpectCommandFailure(servertest.CommandErrorMessageForTest("commands.function.scheduled.no_functions", "t:broken"))
	c.Disconnect()
}

// TestScheduleRefusals checks the refusals: a time of zero, a negative time,
// and a function that is not loaded.
// vanilla: ScheduleCommand.ERROR_SAME_TICK, FunctionArgument.ERROR_UNKNOWN_FUNCTION
func TestScheduleRefusals(t *testing.T) {
	t.Parallel()
	srv, c, _ := scheduleWorld(t)

	c.ChatCommand("schedule function t:count 0")
	c.ExpectCommandFailure(servertest.CommandErrorMessageForTest("commands.schedule.same_tick"))
	c.ChatCommand("schedule function t:missing 5")
	c.ExpectCommandFailure(servertest.CommandErrorMessageForTest("arguments.function.unknown", "t:missing"))
	c.ChatCommand("schedule function t:count -5")
	c.ExpectCommandError(
		servertest.CommandErrorMessageForTest("argument.time.tick_count_too_low", "0", "-5"),
		servertest.CommandErrorContextForTest("schedule function t:count -5", len("schedule function t:count -5")))
	servertest.RunTicks(t, srv, 6)
	if got := score(t, c, "#a"); got != 0 {
		t.Errorf("count after the refusals = %d, want 0", got)
	}
	c.Disconnect()
}

// TestScheduleWaitsOutAFrozenGame checks that the queue runs at the game
// time: a frozen game does not advance it, so an event comes due only after
// the unfreeze.
// vanilla: ServerLevel.tickTime
func TestScheduleWaitsOutAFrozenGame(t *testing.T) {
	t.Parallel()
	srv, c, _ := scheduleWorld(t)

	schedule(t, c, "t:count 3", "commands.schedule.created.function", "t:count", 3)
	c.ChatCommand("tick freeze")
	c.Settle()
	servertest.RunTicks(t, srv, 10)
	if got := score(t, c, "#a"); got != 0 {
		t.Fatalf("count in a frozen game = %d, want 0", got)
	}
	c.ChatCommand("tick unfreeze")
	c.Settle()
	servertest.RunTicks(t, srv, 3)
	if got := score(t, c, "#a"); got != 1 {
		t.Errorf("count 3 ticks after the unfreeze = %d, want 1", got)
	}
	c.Disconnect()
}

// TestScheduledEventsSurviveARestart saves a pending event, checks the file's
// shape against the vanilla saved data, and runs it on the tick it was due
// after the server comes back.
// vanilla: TimerQueue.CODEC
func TestScheduledEventsSurviveARestart(t *testing.T) {
	t.Parallel()
	srv, c, cfg := scheduleWorld(t)

	c.ChatCommand("schedule function t:count 30")
	servertest.FeedbackComponent(t, c)
	c.ChatCommand("schedule function #t:all 40 append")
	servertest.FeedbackComponent(t, c)
	servertest.RunTicks(t, srv, 10)
	c.Disconnect()
	srv.Shutdown()

	events := readScheduledEvents(t, filepath.Join(cfg.LevelName, "data", "minecraft", "scheduled_events.dat"))
	if len(events) != 2 {
		t.Fatalf("saved %d events, want 2", len(events))
	}
	first, second := events[0].(nbt.Compound), events[1].(nbt.Compound)
	if id, _ := nbt.Get[nbt.String](first, "id"); id != "t:count" {
		t.Errorf("first event id = %q, want t:count", id)
	}
	cb, _ := nbt.Get[nbt.Compound](first, "callback")
	if typ, _ := nbt.Get[nbt.String](cb, "type"); typ != "minecraft:function" {
		t.Errorf("first callback type = %q, want minecraft:function", typ)
	}
	if fn, _ := nbt.Get[nbt.String](cb, "id"); fn != "t:count" {
		t.Errorf("first callback id = %q, want t:count", fn)
	}
	cb2, _ := nbt.Get[nbt.Compound](second, "callback")
	if typ, _ := nbt.Get[nbt.String](cb2, "type"); typ != "minecraft:function_tag" {
		t.Errorf("second callback type = %q, want minecraft:function_tag", typ)
	}
	if id, _ := nbt.Get[nbt.String](second, "id"); id != "#t:all" {
		t.Errorf("second event id = %q, want #t:all", id)
	}
	t1, _ := nbt.Get[nbt.Long](first, "trigger_time")
	t2, _ := nbt.Get[nbt.Long](second, "trigger_time")
	if t2-t1 != 10 {
		t.Errorf("trigger times %d, %d, want them 10 apart", t1, t2)
	}

	restarted := *cfg
	srv2, c2, _ := startScheduleWorld(t, &restarted, false)
	// 30 ticks from the schedule, 10 run: 20 remain, the join having run
	// frozen.
	age := srv2.WorldAge()
	remaining := int(int64(t1) - age)
	if remaining != 20 {
		t.Fatalf("the first event has %d ticks left after the restart, want 20", remaining)
	}
	servertest.RunTicks(t, srv2, remaining-1)
	if got := score(t, c2, "#a"); got != 0 {
		t.Fatalf("count one tick before the reloaded event = %d, want 0", got)
	}
	servertest.RunTicks(t, srv2, 1)
	if got := score(t, c2, "#a"); got != 1 {
		t.Errorf("count on the reloaded event's tick = %d, want 1", got)
	}
	servertest.RunTicks(t, srv2, int(t2-t1))
	if got := score(t, c2, "#a"); got != 2 {
		t.Errorf("count on the reloaded tag's tick = %d, want 2", got)
	}
	if got := score(t, c2, "#b"); got != 1 {
		t.Errorf("second counter on the reloaded tag's tick = %d, want 1", got)
	}
	c2.Disconnect()
}

// TestScheduledOverdueEventRunsOnFirstTick checks that an event whose time
// passed while the server was down runs on the first tick after the restart.
// vanilla: TimerQueue.tick
func TestScheduledOverdueEventRunsOnFirstTick(t *testing.T) {
	t.Parallel()
	srv, c, cfg := scheduleWorld(t)

	c.ChatCommand("schedule function t:count 100000")
	servertest.FeedbackComponent(t, c)
	c.Disconnect()
	srv.Shutdown()

	path := filepath.Join(cfg.LevelName, "data", "minecraft", "scheduled_events.dat")
	events := readScheduledEvents(t, path)
	if len(events) != 1 {
		t.Fatalf("saved %d events, want 1", len(events))
	}
	events[0].(nbt.Compound)["trigger_time"] = nbt.Long(1)
	root := nbt.Compound{
		"DataVersion": nbt.Int(1),
		"data":        nbt.Compound{"events": nbt.List{Elem: nbt.TypeCompound, Items: events}},
	}
	raw, err := nbt.Encode("", root)
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	if _, err := gz.Write(raw); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}

	restarted := *cfg
	srv2, c2, _ := startScheduleWorld(t, &restarted, false)
	servertest.RunTicks(t, srv2, 1)
	if got := score(t, c2, "#a"); got != 1 {
		t.Errorf("count after the first tick with an overdue event = %d, want 1", got)
	}
	servertest.RunTicks(t, srv2, 5)
	if got := score(t, c2, "#a"); got != 1 {
		t.Errorf("count 5 ticks on = %d, want it run once", got)
	}
	c2.Disconnect()
}

// readScheduledEvents decodes the events list of a saved scheduled_events.dat.
func readScheduledEvents(t *testing.T, path string) []nbt.Tag {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	gz, err := gzip.NewReader(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if _, err := buf.ReadFrom(gz); err != nil {
		t.Fatal(err)
	}
	_, tag, err := nbt.Decode(buf.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	root := tag.(nbt.Compound)
	if _, ok := nbt.Get[nbt.Int](root, "DataVersion"); !ok {
		t.Error("saved data has no DataVersion")
	}
	data, _ := nbt.Get[nbt.Compound](root, "data")
	events, _ := nbt.Get[nbt.List](data, "events")
	return events.Items
}

// TestLoadAndTickTags checks that the minecraft:load tag runs once after the
// pack loads and the minecraft:tick tag on every tick that runs.
// vanilla: ServerFunctionManager.tick
func TestLoadAndTickTags(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.LevelName = t.TempDir()
	pack := filepath.Join(cfg.LevelName, "datapacks", "test")
	writeFile(t, filepath.Join(pack, "pack.mcmeta"), `{"pack":{"pack_format":101,"description":"test"}}`)
	writeFile(t, filepath.Join(pack, "data/minecraft/tags/function/load.json"), `{"values":["t:load"]}`)
	writeFile(t, filepath.Join(pack, "data/minecraft/tags/function/tick.json"), `{"values":["t:tick"]}`)
	writeFile(t, filepath.Join(pack, "data/t/function/load.mcfunction"),
		"scoreboard objectives add cnt dummy\nscoreboard players add #a cnt 1\nscoreboard players add #b cnt 0\n")
	writeFile(t, filepath.Join(pack, "data/t/function/tick.mcfunction"), "scoreboard players add #b cnt 1\n")
	srv, connect := servertest.Start(t, cfg, servertest.OpsListing(t, "Testificate"))
	servertest.FreezeUntilHeld(t, srv)
	c := connect().Join("Testificate")
	servertest.HoldTicks(t, srv)
	servertest.RunTicks(t, srv, 5)
	if got := score(t, c, "#a"); got != 1 {
		t.Errorf("load tag ran %d times, want 1", got)
	}
	b := score(t, c, "#b")
	servertest.RunTicks(t, srv, 7)
	if got := score(t, c, "#b"); got != b+7 {
		t.Errorf("tick tag count went %d -> %d over 7 ticks, want +7", b, got)
	}
	c.Disconnect()
}
