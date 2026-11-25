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

// packWorld puts the packs "a" and "b" on disk, both defining x:f (a
// returns 1, b returns 2) and a alone x:onlya, and returns the config of a
// world holding them.
func packWorld(t *testing.T) *config.Config {
	t.Helper()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.LevelName = t.TempDir()
	for name, ret := range map[string]string{"a": "1", "b": "2"} {
		pack := filepath.Join(cfg.LevelName, "datapacks", name)
		writeFile(t, filepath.Join(pack, "pack.mcmeta"), `{"pack":{"description":"pack `+name+`"}}`)
		writeFile(t, filepath.Join(pack, "data", "x", "function", "f.mcfunction"), "return "+ret+"\n")
	}
	writeFile(t, filepath.Join(cfg.LevelName, "datapacks", "a", "data", "x", "function", "onlya.mcfunction"), "return 7\n")
	return cfg
}

// joinPacks starts a server on cfg and joins an operator.
func joinPacks(t *testing.T, cfg *config.Config) (*server.Server, *servertest.Client) {
	t.Helper()
	srv, c := servertest.Join(t, cfg, "Testificate", servertest.OpsListing(t, "Testificate"))
	return srv, c
}

// packIDs reads the ids out of a /datapack list line.
func packIDs(t *testing.T, line protocol.Text) []string {
	t.Helper()
	links := line.Extra
	if len(links) == 0 {
		links = []protocol.Text{line}
	}
	var ids []string
	for _, l := range links {
		if l.Translate == "chat.square_brackets" {
			ids = append(ids, l.With[0].With[0].Text)
		}
	}
	return ids
}

// expectPackList reads one /datapack list line and checks its key and ids.
func expectPackList(t *testing.T, c *servertest.Client, key string, want ...string) {
	t.Helper()
	checkPackList(t, c.ExpectPlay(protocol.IDPlaySystemChat), key, want...)
}

// checkPackList checks a /datapack list line already read.
func checkPackList(t *testing.T, data []byte, key string, want ...string) {
	t.Helper()
	r := protocol.NewReader(data)
	got, ok := protocol.ReadText(r)
	if !ok {
		t.Fatalf("decode system chat component: %v", r.Err())
	}
	if got.Translate != key {
		t.Fatalf("list line %q, want %q", got.Translate, key)
	}
	if n := number(got.With[0]); n != int64(len(want)) {
		t.Errorf("%s counts %d packs, want %d", key, n, len(want))
	}
	if ids := packIDs(t, got.With[1]); !slices.Equal(ids, want) {
		t.Errorf("%s lists %v, want %v", key, ids, want)
	}
}

// expectFunctionReturns runs /function and checks the value it returns, or
// that the function is unknown when want is zero.
func expectFunctionReturns(t *testing.T, c *servertest.Client, id string, want int64) {
	t.Helper()
	c.ChatCommand("function " + id)
	if want == 0 {
		c.ExpectCommandFailure(servertest.CommandErrorMessageForTest("arguments.function.unknown", id))
		return
	}
	c.ExpectCommandFeedback("commands.function.scheduled.single", id)
	expectFunctionResult(c, id, want)
}

// TestDatapackListEnableDisable checks that /datapack lists the packs in
// layer order, that disabling one drops its functions, and that enabling it
// with first, last, before and after changes which pack wins a duplicate
// function id.
// vanilla: DataPackCommand
func TestDatapackListEnableDisable(t *testing.T) {
	t.Parallel()
	_, c := joinPacks(t, packWorld(t))

	c.ChatCommand("datapack list")
	expectPackList(t, c, "commands.datapack.list.enabled.success", "vanilla", "file/a", "file/b")
	c.ExpectCommandFeedback("commands.datapack.list.available.none")
	expectFunctionReturns(t, c, "x:f", 2)
	expectFunctionReturns(t, c, "x:onlya", 7)

	c.ChatCommand(`datapack disable "file/a"`)
	if got := servertest.FeedbackComponent(t, c); got.Translate != "commands.datapack.modify.disable" {
		t.Fatalf("disable said %+v", got)
	} else if len(got.With) != 1 || got.With[0].Color != protocol.ColorGreen {
		t.Errorf("disable link = %+v, want green", got.With)
	}
	expectFunctionReturns(t, c, "x:onlya", 0)
	c.ChatCommand("datapack list available")
	expectPackList(t, c, "commands.datapack.list.available.success", "file/a")
	c.ChatCommand("datapack list enabled")
	expectPackList(t, c, "commands.datapack.list.enabled.success", "vanilla", "file/b")

	enable := func(suffix string, order ...string) {
		t.Helper()
		c.ChatCommand(`datapack enable "file/a"` + suffix)
		if got := servertest.FeedbackComponent(t, c); got.Translate != "commands.datapack.modify.enable" {
			t.Fatalf("enable%s said %+v", suffix, got)
		}
		c.ChatCommand("datapack list enabled")
		expectPackList(t, c, "commands.datapack.list.enabled.success", order...)
		c.ChatCommand(`datapack disable "file/a"`)
		servertest.FeedbackComponent(t, c)
	}
	enable("", "vanilla", "file/b", "file/a")
	enable(" last", "vanilla", "file/b", "file/a")
	enable(" first", "file/a", "vanilla", "file/b")
	enable(` before "file/b"`, "vanilla", "file/a", "file/b")
	enable(` after "file/b"`, "vanilla", "file/b", "file/a")
	enable(` after vanilla`, "vanilla", "file/a", "file/b")

	// The last pack wins a duplicate id.
	c.ChatCommand(`datapack enable "file/a"`)
	servertest.FeedbackComponent(t, c)
	expectFunctionReturns(t, c, "x:f", 1)
	expectFunctionReturns(t, c, "x:onlya", 7)
	c.ChatCommand(`datapack disable "file/a"`)
	servertest.FeedbackComponent(t, c)
	c.ChatCommand(`datapack enable "file/a" first`)
	servertest.FeedbackComponent(t, c)
	expectFunctionReturns(t, c, "x:f", 2)
}

// TestDatapackErrors checks the refusals: an unknown pack, a pack already
// enabled or not enabled, and a pack that needs a feature flag.
// vanilla: DataPackCommand.getPack
func TestDatapackErrors(t *testing.T) {
	t.Parallel()
	_, c := joinPacks(t, packWorld(t))
	fail := func(command, key string, with ...string) {
		t.Helper()
		c.ChatCommand(command)
		c.ExpectCommandFailure(servertest.CommandErrorMessageForTest(key, with...))
	}
	fail("datapack enable nothing", "commands.datapack.unknown", "nothing")
	fail("datapack disable nothing", "commands.datapack.unknown", "nothing")
	fail(`datapack enable "file/a"`, "commands.datapack.enable.failed", "file/a")
	c.ChatCommand(`datapack disable "file/a"`)
	servertest.FeedbackComponent(t, c)
	fail(`datapack disable "file/a"`, "commands.datapack.disable.failed", "file/a")
	fail(`datapack enable "file/a" before "file/a"`, "commands.datapack.disable.failed", "file/a")
	fail("datapack enable trade_rebalance", "commands.datapack.enable.failed.no_flags",
		"trade_rebalance", "minecraft:trade_rebalance")
	fail("datapack disable minecart_improvements", "commands.datapack.disable.failed", "minecart_improvements")
}

// TestDatapackNeedsPermission checks that a player without a permission
// level has no /datapack, and that an operator below level 4 has no create.
func TestDatapackNeedsPermission(t *testing.T) {
	t.Parallel()
	cfg := packWorld(t)
	_, c := servertest.Join(t, cfg, "Testificate")
	const list = "datapack list"
	c.ChatCommand(list)
	c.ExpectCommandError(
		servertest.CommandErrorMessageForTest("command.unknown.command"),
		servertest.CommandErrorContextForTest(list, 0))

	cfg2 := packWorld(t)
	_, c2 := servertest.Join(t, cfg2, "Testificate", servertest.WriteOpsFile(t, servertest.OpRecord{
		UUID: protocol.OfflineUUID("Testificate").String(), Name: "Testificate", Level: 2,
	}))
	c2.ChatCommand("datapack list")
	expectPackList(t, c2, "commands.datapack.list.enabled.success", "vanilla", "file/a", "file/b")
	c2.ExpectCommandFeedback("commands.datapack.list.available.none")
	const create = `datapack create x "d"`
	c2.ChatCommand(create)
	c2.ExpectCommandError(
		servertest.CommandErrorMessageForTest("command.unknown.argument"),
		servertest.CommandErrorContextForTest(create, len("datapack ")))
	if _, err := os.Stat(filepath.Join(cfg2.LevelName, "datapacks", "x")); err == nil {
		t.Errorf("a level 2 operator created a pack")
	}
}

// TestDatapackEnabledListSurvivesARestart checks that the enabled list and
// its order are kept in level.dat and the functions follow it after a
// restart, and that a pack disabled before it stays out.
// vanilla: WorldData.setDataConfiguration
func TestDatapackEnabledListSurvivesARestart(t *testing.T) {
	t.Parallel()
	cfg := packWorld(t)
	srv, c := joinPacks(t, cfg)
	c.ChatCommand(`datapack disable "file/b"`)
	servertest.FeedbackComponent(t, c)
	c.ChatCommand(`datapack enable "file/a" first`)
	servertest.FeedbackComponent(t, c)
	c.ChatCommand(`datapack disable "file/a"`)
	servertest.FeedbackComponent(t, c)
	c.ChatCommand(`datapack enable "file/b" first`)
	servertest.FeedbackComponent(t, c)
	c.ChatCommand(`datapack enable "file/a" last`)
	servertest.FeedbackComponent(t, c)
	c.Disconnect()
	srv.Shutdown()

	restarted := *cfg
	_, c2 := joinPacks(t, &restarted)
	c2.ChatCommand("datapack list")
	expectPackList(t, c2, "commands.datapack.list.enabled.success", "file/b", "vanilla", "file/a")
	c2.ExpectCommandFeedback("commands.datapack.list.available.none")
	expectFunctionReturns(t, c2, "x:f", 1)

	c2.ChatCommand(`datapack disable "file/b"`)
	servertest.FeedbackComponent(t, c2)
	c2.Disconnect()
}

// TestDatapackCreate checks that /datapack create makes a pack folder with a
// pack.mcmeta that loads: the pack lists as available, enabling it loads a
// function put in its folder, and the name checks refuse an existing pack
// and a name no folder may have.
// vanilla: DataPackCommand.createPack
func TestDatapackCreate(t *testing.T) {
	t.Parallel()
	cfg := packWorld(t)
	_, c := joinPacks(t, cfg)

	c.ChatCommand(`datapack create fresh "a new pack"`)
	c.ExpectCommandFeedback("commands.datapack.create.success", "fresh")
	meta, err := os.ReadFile(filepath.Join(cfg.LevelName, "datapacks", "fresh", "pack.mcmeta"))
	if err != nil {
		t.Fatal(err)
	}
	want := "{\n  \"pack\": {\n    \"description\": \"a new pack\",\n    \"min_format\": 121,\n    \"max_format\": 121\n  }\n}"
	if string(meta) != want {
		t.Errorf("pack.mcmeta = %q, want %q", meta, want)
	}
	if info, err := os.Stat(filepath.Join(cfg.LevelName, "datapacks", "fresh", "data")); err != nil || !info.IsDir() {
		t.Errorf("the new pack has no data folder: %v", err)
	}

	c.ChatCommand("datapack list available")
	expectPackList(t, c, "commands.datapack.list.available.success", "file/fresh")
	writeFile(t, filepath.Join(cfg.LevelName, "datapacks", "fresh", "data", "x", "function", "new.mcfunction"), "return 5\n")
	c.ChatCommand(`datapack enable "file/fresh"`)
	servertest.FeedbackComponent(t, c)
	expectFunctionReturns(t, c, "x:new", 5)

	fail := func(command, key, name string) {
		t.Helper()
		c.ChatCommand(command)
		c.ExpectCommandFailure(servertest.CommandErrorMessageForTest(key, name))
	}
	fail(`datapack create fresh "again"`, "commands.datapack.create.already_exists", "fresh")
	fail(`datapack create "Bad Name" "d"`, "commands.datapack.create.invalid_name", "Bad Name")
	fail(`datapack create .. "d"`, "commands.datapack.create.invalid_name", "..")
	fail(`datapack create con "d"`, "commands.datapack.create.invalid_full_name", "con")
	fail(`datapack create trailing. "d"`, "commands.datapack.create.invalid_full_name", "trailing.")
}
