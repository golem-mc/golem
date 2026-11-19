package commands

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
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

// TestReloadKeepsDisabledPacks checks that a pack /datapack disabled stays
// disabled across /reload.
// vanilla: ReloadCommand.discoverNewPacks
func TestReloadKeepsDisabledPacks(t *testing.T) {
	t.Parallel()
	_, c := joinPacks(t, packWorld(t))
	c.ChatCommand(`datapack disable "file/a"`)
	servertest.FeedbackComponent(t, c)
	c.ChatCommand("reload")
	c.ExpectCommandFeedback("commands.reload.success")
	expectFunctionReturns(t, c, "x:onlya", 0)
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

// TestReloadResendsTagsAndRecipes checks that a successful /reload sends the
// player Update Tags, Update Recipes and the recipe book again, in that
// order, and that a /datapack change does too.
// vanilla: PlayerList.reloadResources
func TestReloadResendsTagsAndRecipes(t *testing.T) {
	t.Parallel()
	_, c := joinPacks(t, packWorld(t))
	c.WatchBook = true
	for _, command := range []string{"reload", `datapack disable "file/a"`} {
		c.ChatCommand(command)
		servertest.FeedbackComponent(t, c)

		tags := protocol.NewReader(c.ExpectPlay(protocol.IDPlayUpdateTags))
		if got, want := tags.ReadVarInt(), int32(len(registry.Tags)+len(registry.BuiltinTags)); got != want {
			t.Fatalf("%s: tagged registries = %d, want %d", command, got, want)
		}
		recipes := protocol.NewReader(c.ExpectPlay(protocol.IDPlayUpdateRecipes))
		if got, want := recipes.ReadVarInt(), int32(len(registry.RecipePropertySets)); got != want {
			t.Fatalf("%s: property sets = %d, want %d", command, got, want)
		}
		if len(c.ExpectPlay(protocol.IDPlayRecipeBookSettings)) == 0 {
			t.Fatalf("%s: empty recipe book settings", command)
		}
		book := protocol.NewReader(c.ExpectPlay(protocol.IDPlayRecipeBookAdd))
		if n := book.ReadVarInt(); n < 1 {
			t.Fatalf("%s: recipe book add carries %d entries, want the player's known recipes", command, n)
		}
	}
}
