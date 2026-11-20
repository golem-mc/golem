package advancements

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/advancement"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// The pack layer is process-wide, so these tests are not parallel with each
// other and put the vanilla tree back when they finish.

// packAdvancement is a displayed root advancement, with its own tab, that is
// done by holding the item: its one criterion is "got".
func packAdvancement(item string) string { return packAdvancementFor(item, item) }

// packAdvancementFor is packAdvancement with an icon other than the item.
func packAdvancementFor(icon, item string) string {
	return `{"display":{"icon":{"id":"minecraft:` + icon + `"},"title":{"translate":"pk.title"},` +
		`"description":{"translate":"pk.desc"},"background":"minecraft:gui/advancements/backgrounds/stone"},` +
		`"criteria":{"got":{"trigger":"minecraft:inventory_changed","conditions":{"items":[{"items":"minecraft:` + item + `"}]}}}}`
}

func writePackFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// packConfig is a survival world with one data pack, whose advancement
// folder is returned.
func packConfig(t *testing.T) (*config.Config, string) {
	t.Helper()
	t.Cleanup(func() { advancement.ApplyPack(nil) })
	cfg := servertest.SurvivalTestConfig()
	cfg.LevelName = t.TempDir()
	pack := filepath.Join(cfg.LevelName, "datapacks", "a")
	writePackFile(t, filepath.Join(pack, "pack.mcmeta"), `{"pack":{"description":"a"}}`)
	return cfg, filepath.Join(pack, "data")
}

func joinOp(t *testing.T, cfg *config.Config, name string) (*server.Server, *servertest.Client) {
	t.Helper()
	srv, connect := servertest.Start(t, cfg, servertest.OpsListing(t, name))
	c, _ := join(t, connect, name)
	return srv, c
}

func give(srv *server.Server, t *testing.T, name, item string) {
	t.Helper()
	srv.GiveSlot(name, player.HotbarSlot, servertest.ItemStack(t, "minecraft:"+item, 1))
}

// TestPackAdvancementIsGrantedByItsTrigger: an advancement a data pack adds
// is awarded when its trigger fires and its tab is sent to the player.
// vanilla: ServerAdvancementManager.apply, InventoryChangeTrigger
func TestPackAdvancementIsGrantedByItsTrigger(t *testing.T) {
	cfg, data := packConfig(t)
	writePackFile(t, filepath.Join(data, "pk", "advancement", "stick.json"), packAdvancement("stick"))
	// A file that does not decode is skipped, not the whole load.
	writePackFile(t, filepath.Join(data, "pk", "advancement", "broken.json"), `{"criteria":`)
	srv, c := joinOp(t, cfg, "Packer")

	give(srv, t, "Packer", "dirt")
	c.Settle()
	if srv.AdvancementObtained("Packer", "pk:stick", "got") {
		t.Fatal("holding dirt obtained the stick advancement")
	}
	give(srv, t, "Packer", "stick")
	u := awaitUpdate(t, srv, c)
	if !srv.AdvancementObtained("Packer", "pk:stick", "got") {
		t.Error("holding a stick did not obtain pk:stick")
	}
	added, ok := u.added["pk:stick"]
	if !ok || added.title != "pk.title" || added.background == "" {
		t.Errorf("the update added %+v, want pk:stick with its title and tab background", u.added)
	}
	if u.progress["pk:stick"]["got"] == nil {
		t.Errorf("the update's progress for pk:stick = %v, want got obtained", u.progress["pk:stick"])
	}
}

// TestReloadKeepsProgressAndDropsRemovedAdvancements: /reload rebuilds the
// tree from the packs and every player's progress over it, then resets the
// client's tree: progress of an advancement that remains is kept, a new
// advancement works, and a removed one is gone from the tree sent.
// vanilla: PlayerAdvancements.reload
func TestReloadKeepsProgressAndDropsRemovedAdvancements(t *testing.T) {
	cfg, data := packConfig(t)
	dir := filepath.Join(data, "pk", "advancement")
	writePackFile(t, filepath.Join(dir, "stick.json"), packAdvancement("stick"))
	writePackFile(t, filepath.Join(dir, "keep.json"), packAdvancement("bone"))
	srv, c := joinOp(t, cfg, "Packer")
	give(srv, t, "Packer", "stick")
	awaitUpdate(t, srv, c)
	srv.GiveSlot("Packer", player.HotbarSlot+1, servertest.ItemStack(t, "minecraft:bone", 1))
	awaitUpdate(t, srv, c)

	writePackFile(t, filepath.Join(dir, "new.json"), packAdvancement("clay_ball"))
	if err := os.Remove(filepath.Join(dir, "stick.json")); err != nil {
		t.Fatal(err)
	}
	out := run(t, srv, c, "reload")
	var reset *sentUpdate
	for i := range out.updates {
		if out.updates[i].reset {
			reset = &out.updates[i]
		}
	}
	if reset == nil {
		t.Fatalf("/reload sent no resetting update: %+v", out.updates)
	}
	if _, ok := reset.added["pk:stick"]; ok || reset.progress["pk:stick"] != nil {
		t.Errorf("the reset update still carries the removed pk:stick: %+v", reset)
	}
	if reset.added["pk:keep"].title != "pk.title" || reset.progress["pk:keep"]["got"] == nil {
		t.Errorf("the reset update = %+v, want pk:keep shown with got obtained", reset)
	}
	if !srv.AdvancementObtained("Packer", "pk:keep", "got") || srv.AdvancementObtained("Packer", "pk:stick", "got") {
		t.Error("progress after the reload: pk:keep lost, or pk:stick kept")
	}

	srv.GiveSlot("Packer", player.HotbarSlot+2, servertest.ItemStack(t, "minecraft:clay_ball", 1))
	awaitUpdate(t, srv, c)
	if !srv.AdvancementObtained("Packer", "pk:new", "got") {
		t.Error("the advancement added by the reload is not awarded")
	}
}

// TestPackRecipeAdvancementUnlocksItsRecipe: a pack's recipes/ advancement is
// loaded and fired like vanilla's, so holding its ingredient adds the pack
// recipe to the recipe book, and a pack overriding a vanilla recipe
// advancement with an impossible criterion stops it unlocking.
// vanilla: ServerAdvancementManager.apply, AdvancementRewards.grant
func TestPackRecipeAdvancementUnlocksItsRecipe(t *testing.T) {
	t.Cleanup(func() { registry.ApplyPackRecipes(registry.PackRecipes{}) })
	cfg, data := packConfig(t)
	writePackFile(t, filepath.Join(data, "t", "recipe", "wand.json"),
		`{"type":"minecraft:crafting_shaped","pattern":["a","b"],"key":{"a":"minecraft:stick","b":"minecraft:diamond"},`+
			`"result":{"id":"minecraft:diamond","count":1}}`)
	writePackFile(t, filepath.Join(data, "t", "advancement", "recipes", "misc", "wand.json"),
		`{"criteria":{"x":{"trigger":"minecraft:inventory_changed","conditions":{"items":[{"items":"minecraft:diamond"}]}}},`+
			`"rewards":{"recipes":["t:wand"]}}`)
	writePackFile(t, filepath.Join(data, "minecraft", "advancement", "recipes", "misc", "stick.json"),
		`{"criteria":{"x":{"trigger":"minecraft:impossible"}},"rewards":{"recipes":["minecraft:stick"]}}`)
	srv, connect := servertest.Start(t, cfg, servertest.OpsListing(t, "Packer"))
	c := connect()
	c.WatchBook = true
	c.Login("Packer")
	c.Configure()
	c.ReadJoin(func(int32, []byte) {})
	c.MarkLoaded()
	c.Settle()

	give(srv, t, "Packer", "oak_planks")
	servertest.RunTicks(t, srv, 3)
	c.Settle()
	if srv.RecipeKnown("Packer", "minecraft:stick") {
		t.Error("oak planks unlocked the stick recipe despite the impossible override")
	}
	give(srv, t, "Packer", "diamond")
	servertest.RunTicks(t, srv, 3)
	c.Settle()
	if !srv.RecipeKnown("Packer", "t:wand") {
		t.Error("holding a diamond did not unlock the pack recipe")
	}
}

// TestPackTickAdvancementTestsItsPlayerConditions: a tick criterion with a
// player predicate is obtained only while the predicate holds of the player.
// vanilla: SimpleCriterionTrigger.SimpleInstance.player
func TestPackTickAdvancementTestsItsPlayerConditions(t *testing.T) {
	cfg, data := packConfig(t)
	dir := filepath.Join(data, "pk", "advancement")
	tick := func(player string) string {
		return `{"criteria":{"c":{"trigger":"minecraft:tick","conditions":{"player":[` + player + `]}}}}`
	}
	writePackFile(t, filepath.Join(dir, "sneak.json"), tick(
		`{"condition":"minecraft:entity_properties","entity":"this","predicate":{"minecraft:flags":{"is_sneaking":true}}}`))
	writePackFile(t, filepath.Join(dir, "score.json"), tick(
		`{"condition":"minecraft:entity_scores","entity":"this","scores":{"x":1}}`))
	srv, c := joinOp(t, cfg, "Packer")
	c.ChatCommand("scoreboard objectives add x dummy")
	c.Settle()

	servertest.RunTicks(t, srv, 5)
	if srv.AdvancementObtained("Packer", "pk:sneak", "c") || srv.AdvancementObtained("Packer", "pk:score", "c") {
		t.Fatal("a tick advancement was obtained while its player condition fails")
	}
	c.SendSneak(true)
	servertest.UntilTicks(t, srv, 20, "the sneak advancement", func() bool { return srv.AdvancementObtained("Packer", "pk:sneak", "c") })
	if srv.AdvancementObtained("Packer", "pk:score", "c") {
		t.Fatal("the score advancement was obtained with the score unset")
	}
	c.ChatCommand("scoreboard players set Packer x 1")
	servertest.UntilTicks(t, srv, 20, "the score advancement", func() bool { return srv.AdvancementObtained("Packer", "pk:score", "c") })
}
