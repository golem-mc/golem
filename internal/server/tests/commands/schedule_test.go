package commands

import (
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/golem-mc/golem/internal/config"
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
