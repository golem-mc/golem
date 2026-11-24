package commands

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// TestReloadPicksUpNewFunctionsAndPacks checks that /reload rereads the
// function files of an enabled pack and enables a pack folder added since
// the last scan.
// vanilla: ReloadCommand
func TestReloadPicksUpNewFunctionsAndPacks(t *testing.T) {
	t.Parallel()
	cfg := packWorld(t)
	_, c := joinPacks(t, cfg)
	expectFunctionReturns(t, c, "x:f", 2)
	expectFunctionReturns(t, c, "x:late", 0)

	root := filepath.Join(cfg.LevelName, "datapacks")
	writeFile(t, filepath.Join(root, "a", "data", "x", "function", "late.mcfunction"), "return 5\n")
	writeFile(t, filepath.Join(root, "a", "data", "x", "function", "f.mcfunction"), "return 9\n")
	writeFile(t, filepath.Join(root, "c", "pack.mcmeta"), `{"pack":{"description":"pack c"}}`)
	writeFile(t, filepath.Join(root, "c", "data", "x", "function", "fromc.mcfunction"), "return 3\n")

	c.ChatCommand("reload")
	c.ExpectCommandFeedback("commands.reload.success")
	expectFunctionReturns(t, c, "x:late", 5)
	expectFunctionReturns(t, c, "x:f", 2)
	expectFunctionReturns(t, c, "x:fromc", 3)
	c.ChatCommand("datapack list enabled")
	expectPackList(t, c, "commands.datapack.list.enabled.success", "vanilla", "file/a", "file/b", "file/c")
}

// TestReloadFailureKeepsOldState checks that a /reload that fails after a new
// pack was discovered and its functions compiled leaves the loaded functions
// and the enabled packs as they were, and still answers success. A reload
// that fails on a loot table's overflowing weights, after the pack's context
// providers were read, also leaves those providers unpublished.
// vanilla: ReloadCommand.reloadPacks, MinecraftServer.reloadResources,
// WeightedRandom.getTotalWeight
func TestReloadFailureKeepsOldState(t *testing.T) {
	// Not parallel: the context provider layer is process-wide, and another
	// test's pack load would replace the one this test expects to survive.
	cfg := packWorld(t)
	root := filepath.Join(cfg.LevelName, "datapacks")
	writeFile(t, filepath.Join(root, "a", "data", "x", "context_int_provider", "keep.json"), `{"type":"minecraft:constant","value":17}`)
	srv, c := joinPacks(t, cfg)
	writeFile(t, filepath.Join(root, "a", "data", "x", "function", "new.mcfunction"), "return 4\n")
	writeFile(t, filepath.Join(root, "c", "pack.mcmeta"), `{"pack":{"description":"pack c"}}`)
	writeFile(t, filepath.Join(root, "c", "data", "x", "function", "fromc.mcfunction"), "return 3\n")
	tags := filepath.Join(root, "c", "data", "x", "tags", "function")
	writeFile(t, filepath.Join(tags, "ok.json"), `{"values":[]}`)
	if err := os.Symlink(filepath.Join(tags, "missing"), filepath.Join(tags, "broken.json")); err != nil {
		t.Fatal(err)
	}

	c.ChatCommand("reload")
	c.ExpectCommandFeedback("commands.reload.success")
	c.ExpectCommandFailure(servertest.CommandErrorMessageForTest("commands.reload.failure"))
	expectFunctionReturns(t, c, "x:new", 0)
	expectFunctionReturns(t, c, "x:fromc", 0)
	expectFunctionReturns(t, c, "x:onlya", 7)
	c.ChatCommand("datapack list enabled")
	expectPackList(t, c, "commands.datapack.list.enabled.success", "vanilla", "file/a", "file/b")

	// The weights overflow: the failure comes after the pack's tags and
	// context providers were read.
	servertest.HoldTicks(t, srv)
	if err := os.Remove(filepath.Join(tags, "broken.json")); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(root, "c", "data", "x", "context_int_provider", "leak.json"), `{"type":"minecraft:constant","value":73}`)
	writeFile(t, filepath.Join(root, "a", "data", "x", "context_int_provider", "keep.json"), `{"type":"minecraft:constant","value":99}`)
	writeFile(t, filepath.Join(root, "c", "data", "x", "loot_table", "t.json"), `{"pools":[{"rolls":1,"entries":[{"type":"minecraft:item","name":"minecraft:stick",`+
		`"modifier":{"type":"minecraft:set_count","count":{"type":"minecraft:weighted_list","distribution":[`+
		`{"data":1,"weight":2147483647},{"data":2,"weight":1}]}}}]}]}`)
	c.ChatCommand(`datapack enable "file/c"`)
	c.ExpectWithinTicks(srv, 0, protocol.IDPlaySystemChat)
	if got := c.ExpectWithinTicks(srv, 0, protocol.IDPlaySystemChat); !bytes.Equal(got, servertest.CommandErrorMessageForTest("commands.reload.failure")) {
		t.Errorf("overflowing reload answered % X, want the reload failure", got)
	}
	c.ChatCommand("compute default integer x:leak")
	if got := c.ExpectWithinTicks(srv, 0, protocol.IDPlaySystemChat); !bytes.Equal(got,
		servertest.CommandErrorMessageForTest("argument.resource_or_id.no_such_element", "x:leak", "minecraft:context_int_provider")) {
		t.Errorf("a provider of the rejected pack answered % X, want the no such element refusal", got)
	}
	c.ExpectWithinTicks(srv, 0, protocol.IDPlaySystemChat)
	c.ChatCommand("compute default integer x:keep")
	servertest.CheckCommandFeedback(t, c.ExpectWithinTicks(srv, 0, protocol.IDPlaySystemChat), "command.compute.result.unnamed.exact", protocol.Text{Number: new(int64(17))})
	c.ChatCommand("datapack list enabled")
	checkPackList(t, c.ExpectWithinTicks(srv, 0, protocol.IDPlaySystemChat), "commands.datapack.list.enabled.success", "vanilla", "file/a", "file/b")
}
