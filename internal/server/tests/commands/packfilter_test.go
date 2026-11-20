package commands

import (
	"bytes"
	"github.com/golem-mc/golem/internal/protocol"
	"path/filepath"
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// filterMeta writes pack "b"'s pack.mcmeta with a filter section.
func filterMeta(t *testing.T, level, block string) {
	t.Helper()
	writeFile(t, filepath.Join(level, "datapacks", "b", "pack.mcmeta"),
		`{"pack":{"description":"pack b"},"filter":{"block":`+block+`}}`)
}

// TestPackFilterHidesBuiltInRecipes checks that a pack whose filter blocks
// every minecraft recipe leaves no vanilla recipe to give or craft, while its
// own and a lower pack's recipes of other namespaces stay.
// vanilla: MultiPackResourceManager, ResourceFilterSection
func TestPackFilterHidesBuiltInRecipes(t *testing.T) {
	t.Cleanup(func() { registry.ApplyPackRecipes(registry.PackRecipes{}) })
	cfg := packWorld(t)
	filterMeta(t, cfg.LevelName, `[{"namespace":"minecraft","path":"recipe/.*"}]`)
	recipeFile(t, cfg.LevelName, "pk:clay_coal", shapedClayCoal)
	srv, w := servertest.OpenWindowedOps(t, cfg)
	srv.GiveSlot("Testificate", 36, servertest.ItemStack(t, "minecraft:oak_planks", 8))
	srv.GiveSlot("Testificate", 37, servertest.ItemStack(t, "minecraft:clay_ball", 8))
	srv.GiveSlot("Testificate", 38, servertest.ItemStack(t, "minecraft:coal", 8))

	if got := gridResult(t, w, placement{36, 1}, placement{36, 3}); !got.Empty() {
		t.Errorf("two oak planks craft %+v although a pack's filter hides the vanilla recipes", got)
	}
	expectCraft(t, "a recipe of another namespace", gridResult(t, w, placement{37, 1}, placement{38, 2}), "minecraft:diamond", 2)
	if got := runRecipe(w.Client, "recipe give Testificate minecraft:stick"); len(got.adds) != 0 {
		t.Errorf("/recipe give sent %d adds for a hidden vanilla recipe", len(got.adds))
	}
	if got := runRecipe(w.Client, "recipe give Testificate *"); len(got.adds) != 0 {
		t.Errorf("/recipe give * sent %d adds although every vanilla recipe is hidden", len(got.adds))
	}
}

// TestPackFilterHidesLowerPacksOnly checks that a filter hides the files of
// the packs below it and not its own of the same path.
// vanilla: FallbackResourceManager.getResource
func TestPackFilterHidesLowerPacksOnly(t *testing.T) {
	t.Cleanup(func() { registry.ApplyPackRecipes(registry.PackRecipes{}) })
	cfg := packWorld(t)
	filterMeta(t, cfg.LevelName, `[{"namespace":"pk","path":"recipe/c.*"}]`)
	// Pack "a" is below pack "b": its recipe is hidden, "b"'s is not.
	recipeFile(t, cfg.LevelName, "pk:clay_coal", shapedClayCoal)
	writeFile(t, filepath.Join(cfg.LevelName, "datapacks", "b", "data", "pk", "recipe", "clay_planks.json"), clayToPlanks)
	srv, w := servertest.OpenWindowedOps(t, cfg)
	srv.GiveSlot("Testificate", 36, servertest.ItemStack(t, "minecraft:clay_ball", 8))
	srv.GiveSlot("Testificate", 37, servertest.ItemStack(t, "minecraft:coal", 8))

	expectCraft(t, "the filtering pack's own recipe", gridResult(t, w, placement{36, 1}), "minecraft:oak_planks", 7)
	if got := gridResult(t, w, placement{36, 1}, placement{37, 2}); got.Count != 0 {
		t.Errorf("clay and coal craft %+v although a pack above hides the lower pack's recipe", got)
	}
}

// TestPackFilterHidesBuiltInLoot checks that a hidden loot table is absent:
// its block drops nothing while another block still does.
// vanilla: MultiPackResourceManager, ReloadableServerRegistries
func TestPackFilterHidesBuiltInLoot(t *testing.T) {
	t.Cleanup(func() { registry.ApplyPackLoot(nil, nil, nil) })
	cfg := servertest.SurvivalTestConfig()
	cfg.LevelName = t.TempDir()
	writeFile(t, filepath.Join(cfg.LevelName, "datapacks", "b", "pack.mcmeta"),
		`{"pack":{"description":"b"},"filter":{"block":[{"namespace":"minecraft","path":"loot_table/blocks/dirt\\.json"}]}}`)
	srv, c := servertest.Dial(t, cfg, servertest.OpsListing(t, "Testificate"))
	w := servertest.WindowedOf(t, "Testificate", c)
	defer w.Disconnect()
	c.MoveToGround(0.5, 64, 0.5, true)
	c.Settle()
	// An efficiency V shovel breaks both blocks on the click.
	c.ChatCommand(`item replace entity @s hotbar.0 with minecraft:netherite_shovel[minecraft:enchantments={"minecraft:efficiency":5}]`)
	c.Settle()
	servertest.Setblock(t, c, 2, 64, 0, "minecraft:dirt")
	servertest.Setblock(t, c, 0, 64, 2, "minecraft:sand")
	c.BreakBlock(2, 64, 0)
	// A second dig in the same tick is ignored.
	servertest.RunTicks(t, srv, 5)
	c.BreakBlock(0, 64, 2)
	servertest.UntilTicks(t, srv, 40, "the sand's drop", func() bool {
		for _, it := range srv.ItemsInDimension(world.DimensionName) {
			if it.Item == "minecraft:sand" {
				return true
			}
		}
		return false
	})
	for _, it := range srv.ItemsInDimension(world.DimensionName) {
		if it.Item == "minecraft:dirt" {
			t.Errorf("the broken dirt dropped %+v although a pack hides its loot table", it)
		}
	}
}

// TestPackFilterEmptiesABuiltInTag checks that a hidden built-in tag file
// leaves the tag empty while the other tags stay.
// vanilla: TagLoader.load
func TestPackFilterEmptiesABuiltInTag(t *testing.T) {
	t.Cleanup(func() { registry.ApplyPackTags(nil) })
	cfg := packWorld(t)
	filterMeta(t, cfg.LevelName, `[{"namespace":"minecraft","path":"tags/block/dirt\\.json"}]`)
	_, c := joinPacks(t, cfg)

	c.ChatCommand("setblock 0 80 0 minecraft:dirt")
	servertest.FeedbackComponent(t, c)
	c.ChatCommand("execute if block 0 80 0 #minecraft:dirt")
	if got := c.ExpectPlay(protocol.IDPlaySystemChat); !bytes.Equal(got, servertest.CommandErrorMessageForTest("commands.execute.conditional.fail")) {
		t.Errorf("the hidden dirt tag still holds dirt: % X", got)
	}
	c.ChatCommand("execute if block 0 80 0 #minecraft:mineable/shovel run say hi")
	if got := c.ExpectPlay(protocol.IDPlayDisguisedChat); !bytes.Equal(got, sayPayloadForTest("Testificate", "hi")) {
		t.Errorf("another tag lost dirt: % X", got)
	}
}
