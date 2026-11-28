package commands

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
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
// that fails on overflowing weights in a loot table, a predicate or an item
// modifier keeps the old selection too, and the pack's context providers
// stay unpublished, also while the load is still validating.
// vanilla: ReloadCommand.reloadPacks, MinecraftServer.reloadResources,
// WeightedRandom.getTotalWeight
func TestReloadFailureKeepsOldState(t *testing.T) {
	// Not parallel: the context provider layer is process-wide, and another
	// test's pack load would replace the one this test expects to survive.
	cfg := packWorld(t)
	root := filepath.Join(cfg.LevelName, "datapacks")
	writeFile(t, filepath.Join(root, "a", "data", "x", "context_int_provider", "keep.json"), `{"type":"minecraft:constant","value":17}`)
	srv, c := servertest.Join(t, cfg, "Testificate", servertest.OpsListing(t, "Testificate", "Other"))
	other := servertest.JoinFeedbackWorld(t, func() *servertest.Client { return servertest.DialServer(t, srv) }, "Other", []*servertest.Client{c})
	writeFile(t, filepath.Join(root, "a", "data", "x", "function", "new.mcfunction"), "return 4\n")
	writeFile(t, filepath.Join(root, "c", "pack.mcmeta"), `{"pack":{"description":"pack c"}}`)
	writeFile(t, filepath.Join(root, "c", "data", "x", "function", "fromc.mcfunction"), "return 3\n")
	// Packs d and e each hold one overflowing resource of their own.
	const overflow = `{"type":"minecraft:weighted_list","distribution":[{"data":1,"weight":2147483647},{"data":2,"weight":1}]}`
	writeFile(t, filepath.Join(root, "d", "pack.mcmeta"), `{"pack":{"description":"pack d"}}`)
	writeFile(t, filepath.Join(root, "d", "data", "x", "predicate", "p.json"), `{"type":"minecraft:int_value_check","value":`+overflow+`,"test":1}`)
	writeFile(t, filepath.Join(root, "e", "pack.mcmeta"), `{"pack":{"description":"pack e"}}`)
	writeFile(t, filepath.Join(root, "e", "data", "x", "item_modifier", "m.json"), `{"type":"minecraft:set_count","count":`+overflow+`}`)
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

	servertest.HoldTicks(t, srv)
	for _, pack := range []string{"file/d", "file/e"} {
		c.ChatCommand(`datapack enable "` + pack + `"`)
		c.ExpectWithinTicks(srv, 0, protocol.IDPlaySystemChat)
		if got := c.ExpectWithinTicks(srv, 0, protocol.IDPlaySystemChat); !bytes.Equal(got, servertest.CommandErrorMessageForTest("commands.reload.failure")) {
			t.Errorf("enabling %s answered % X, want the reload failure", pack, got)
		}
		c.ChatCommand("datapack list enabled")
		checkPackList(t, c.ExpectWithinTicks(srv, 0, protocol.IDPlaySystemChat), "commands.datapack.list.enabled.success", "vanilla", "file/a", "file/b")
	}

	// The weights overflow in a loot table: the failure comes after the
	// pack's tags and context providers were read. The table is a FIFO, so
	// the load waits on it while the other operator reads the live state.
	if err := os.Remove(filepath.Join(tags, "broken.json")); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(root, "c", "data", "x", "context_int_provider", "leak.json"), `{"type":"minecraft:constant","value":73}`)
	writeFile(t, filepath.Join(root, "a", "data", "x", "context_int_provider", "keep.json"), `{"type":"minecraft:constant","value":99}`)
	table := filepath.Join(root, "c", "data", "x", "loot_table", "t.json")
	if err := os.MkdirAll(filepath.Dir(table), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mkfifo(table, 0o644); err != nil {
		t.Fatal(err)
	}
	c.ChatCommand(`datapack enable "file/c"`)
	// Opening the write end waits until the load opens the table.
	fifo, err := os.OpenFile(table, os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	// A failure below must not leave the load waiting on the table.
	t.Cleanup(func() { _ = fifo.Close() })
	other.Drain() // the admin lines of the commands so far
	other.ChatCommand("compute default integer x:keep")
	servertest.CheckCommandFeedback(t, other.ExpectWithinTicks(srv, 0, protocol.IDPlaySystemChat), "command.compute.result.named.exact", protocol.Text{Text: "x:keep"}, protocol.Text{Number: new(int64(17))})
	other.ChatCommand("datapack list enabled")
	checkPackList(t, other.ExpectWithinTicks(srv, 0, protocol.IDPlaySystemChat), "commands.datapack.list.enabled.success", "vanilla", "file/a", "file/b")
	if _, err := fifo.WriteString(`{"pools":[{"rolls":1,"entries":[{"type":"minecraft:item","name":"minecraft:stick",` +
		`"modifier":{"type":"minecraft:set_count","count":` + overflow + `}}]}]}`); err != nil {
		t.Fatal(err)
	}
	if err := fifo.Close(); err != nil {
		t.Fatal(err)
	}
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
	servertest.CheckCommandFeedback(t, c.ExpectWithinTicks(srv, 0, protocol.IDPlaySystemChat), "command.compute.result.named.exact", protocol.Text{Text: "x:keep"}, protocol.Text{Number: new(int64(17))})
	c.ChatCommand("datapack list enabled")
	checkPackList(t, c.ExpectWithinTicks(srv, 0, protocol.IDPlaySystemChat), "commands.datapack.list.enabled.success", "vanilla", "file/a", "file/b")
}

// badPackRecipes are recipe files vanilla's codecs refuse.
// vanilla: ItemStackTemplate.CODEC, Recipe.CODEC
var badPackRecipes = map[string]string{
	"air":    `{"type":"minecraft:crafting_shapeless","ingredients":["minecraft:stick"],"result":{"id":"minecraft:air"}}`,
	"zero":   `{"type":"minecraft:crafting_shapeless","ingredients":["minecraft:stick"],"result":{"id":"minecraft:stone","count":0}}`,
	"stone":  `{"type":"minecraft:stonecutting","ingredient":"minecraft:stone","result":{"id":"minecraft:stone_slab","count":200}}`,
	"smelt":  `{"type":"minecraft:smelting","ingredient":"minecraft:sand","result":{"id":"minecraft:air"}}`,
	"hover":  `{"type":"minecraft:crafting_shapeless","ingredients":["minecraft:stick"],"result":{"id":"minecraft:stone","components":{"minecraft:custom_name":{"text":"x","hover_event":{"action":"show_text"}}}}}`,
	"nowhat": `{"type":"minecraft:crafting_shapeless","ingredients":["minecraft:stick"]}`,
}

const (
	goodPackRecipe = `{"type":"minecraft:crafting_shapeless","ingredients":["minecraft:stick"],"result":{"id":"minecraft:stone"}}`
	goodPackAdv    = `{"criteria":{"c":{"trigger":"minecraft:tick"}}}`
	badPackAdv     = `{"display":{"icon":{"id":"minecraft:stone"},"title":{"text":"x","hover_event":{"action":"show_text"}},"description":"d"},"criteria":{"c":{"trigger":"minecraft:tick"}}}`
)

// TestReloadRefusedPackElementFailsTheReload checks that a pack holding one
// recipe or advancement vanilla's codec refuses fails /reload as a whole:
// the pack's good recipe or advancement stays unknown.
// vanilla: RegistryDataLoader.load, MinecraftServer.reloadResources
func TestReloadRefusedPackElementFailsTheReload(t *testing.T) {
	// Not parallel: the pack layers are process-wide.
	for _, tc := range []struct{ kind, good, bad, cmd, notFound string }{
		{"recipe", goodPackRecipe, badPackRecipes["air"], "recipe give @s zz:good", "recipe.notFound"},
		{"recipe", goodPackRecipe, badPackRecipes["zero"], "recipe give @s zz:good", "recipe.notFound"},
		{"recipe", goodPackRecipe, badPackRecipes["stone"], "recipe give @s zz:good", "recipe.notFound"},
		{"recipe", goodPackRecipe, badPackRecipes["smelt"], "recipe give @s zz:good", "recipe.notFound"},
		{"recipe", goodPackRecipe, badPackRecipes["hover"], "recipe give @s zz:good", "recipe.notFound"},
		{"recipe", goodPackRecipe, badPackRecipes["nowhat"], "recipe give @s zz:good", "recipe.notFound"},
		{"advancement", goodPackAdv, badPackAdv, "advancement grant @s only zz:good", "advancement.advancementNotFound"},
	} {
		cfg := packWorld(t)
		_, c := joinPacks(t, cfg)
		pack := filepath.Join(cfg.LevelName, "datapacks", "zzrec")
		writeFile(t, filepath.Join(pack, "pack.mcmeta"), `{"pack":{"description":"zz"}}`)
		writeFile(t, filepath.Join(pack, "data", "zz", tc.kind, "good.json"), tc.good)
		writeFile(t, filepath.Join(pack, "data", "zz", tc.kind, "bad.json"), tc.bad)
		c.ChatCommand("reload")
		c.ExpectCommandFeedback("commands.reload.success")
		c.ExpectCommandFailure(servertest.CommandErrorMessageForTest("commands.reload.failure"))
		c.ChatCommand(tc.cmd)
		c.ExpectCommandFailure(servertest.CommandErrorMessageForTest(tc.notFound, "zz:good"))
	}
}

// TestStartWithRefusedPackElementFails checks that a server whose enabled
// pack holds an element vanilla's codec refuses does not start, and the
// error names the element.
// vanilla: WorldLoader.load, RegistryDataLoader.load
func TestStartWithRefusedPackElementFails(t *testing.T) {
	cfg := packWorld(t)
	pack := filepath.Join(cfg.LevelName, "datapacks", "a")
	writeFile(t, filepath.Join(pack, "data", "zz", "recipe", "bad.json"), badPackRecipes["air"])
	_, err := server.NewSeeded(cfg, 1)
	if err == nil || !strings.Contains(err.Error(), "zz:bad") {
		t.Fatalf("start with a refused recipe: err = %v, want one naming zz:bad", err)
	}
}
