package commands

import (
	"os"
	"path/filepath"
	"testing"

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
// and the enabled packs as they were, and still answers success.
// vanilla: ReloadCommand.reloadPacks, MinecraftServer.reloadResources
func TestReloadFailureKeepsOldState(t *testing.T) {
	t.Parallel()
	cfg := packWorld(t)
	_, c := joinPacks(t, cfg)
	root := filepath.Join(cfg.LevelName, "datapacks")
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
}
