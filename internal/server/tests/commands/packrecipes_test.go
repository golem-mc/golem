package commands

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// The pack recipe layer is process-wide, so these tests are not parallel with
// each other and clear it afterwards.

// recipeFile writes a recipe under pack "a" of the world cfg.LevelName.
func recipeFile(t *testing.T, level, id, body string) string {
	t.Helper()
	ns, name, _ := strings.Cut(id, ":")
	file := filepath.Join(level, "datapacks", "a", "data", ns, "recipe", name+".json")
	writeFile(t, file, body)
	return file
}

// placement is one ingredient put in the player's 2x2 grid: a hotbar slot's
// item into a grid cell (slots 1 to 4).
type placement struct{ from, cell int16 }

// gridResult puts one of each placed stack into the grid, as a client's
// clicks do, returns what the result slot shows once the last is in, and
// empties the grid again.
func gridResult(t *testing.T, w *servertest.WindowedClient, placements ...placement) protocol.ItemStack {
	t.Helper()
	var result protocol.ItemStack
	collect := func(id int32, data []byte) {
		switch id {
		case protocol.IDPlayContainerSlot:
			r := protocol.NewReader(data)
			window, state, slot := r.ReadVarInt(), r.ReadVarInt(), r.ReadI16()
			stack, _ := r.ReadSlot()
			if window == w.Window {
				w.TrackState(window, state)
				if slot == 0 {
					result = stack
				}
			}
		case protocol.IDPlayContainerContent:
			r := protocol.NewReader(data)
			if window := r.ReadVarInt(); window == w.Window {
				w.TrackState(window, r.ReadVarInt())
			}
		default:
			w.Record(id, data)
		}
	}
	click := func(slot int16, button int8, mode int32) {
		w.Click(slot, button, mode)
		w.SettleAndCollect(collect)
	}
	for _, p := range placements {
		click(p.from, 0, protocol.ClickModePickup)
		click(p.cell, 1, protocol.ClickModePickup)
		click(p.from, 0, protocol.ClickModePickup)
	}
	got := result
	for _, p := range placements {
		click(p.cell, 0, protocol.ClickModeQuickMove)
	}
	return got
}

func expectCraft(t *testing.T, what string, got protocol.ItemStack, item string, count int32) {
	t.Helper()
	if want := servertest.ItemStack(t, item, count); got.ItemID != want.ItemID || got.Count != want.Count {
		name, _ := registry.ItemName(got.ItemID)
		t.Errorf("%s: the result slot shows %s x%d, want %s x%d", what, name, got.Count, item, count)
	}
}

const (
	shapedClayCoal = `{"type":"minecraft:crafting_shaped","pattern":["ab"],"key":{"a":"minecraft:clay_ball","b":"minecraft:coal"},` +
		`"result":{"id":"minecraft:diamond","count":2}}`
	clayToPlanks = `{"type":"minecraft:crafting_shapeless","ingredients":["minecraft:clay_ball"],"result":{"id":"minecraft:oak_planks","count":7}}`
)

// TestPackRecipesCraftAndOverride checks that a data pack's shaped recipe
// crafts in the player's grid, that a pack recipe of a built-in recipe's ID
// replaces it (the built-in one no longer crafts) while the other built-ins
// stay.
// vanilla: RecipeManager.prepare
func TestPackRecipesCraftAndOverride(t *testing.T) {
	t.Cleanup(func() { registry.ApplyPackRecipes(registry.PackRecipes{}) })
	cfg := packWorld(t)
	recipeFile(t, cfg.LevelName, "pk:clay_coal", shapedClayCoal)
	recipeFile(t, cfg.LevelName, "minecraft:oak_planks", clayToPlanks)
	srv, w := servertest.OpenWindowedOps(t, cfg)
	for slot, item := range map[int]string{36: "minecraft:clay_ball", 37: "minecraft:coal", 38: "minecraft:oak_log", 39: "minecraft:birch_log", 40: "minecraft:gold_nugget", 41: "minecraft:jungle_log"} {
		srv.GiveSlot("Testificate", slot, servertest.ItemStack(t, item, 8))
	}

	expectCraft(t, "clay and coal", gridResult(t, w, placement{36, 1}, placement{37, 2}), "minecraft:diamond", 2)
	expectCraft(t, "clay alone", gridResult(t, w, placement{36, 1}), "minecraft:oak_planks", 7)
	if got := gridResult(t, w, placement{38, 1}); !got.Empty() {
		t.Errorf("an oak log crafts %+v although a pack replaced the oak planks recipe", got)
	}
	expectCraft(t, "a birch log", gridResult(t, w, placement{39, 1}), "minecraft:birch_planks", 4)
	expectCraft(t, "a jungle log", gridResult(t, w, placement{41, 1}), "minecraft:jungle_planks", 4)
}

// TestPackRecipesReachTheRecipeBookAndUpdateRecipes checks that /recipe give
// grants a pack recipe's display, that Update Recipes lists the pack's
// stonecutting recipes and the items its cooking recipes take, and that
// /reload sends the changed lists.
// vanilla: RecipeManager.getSynchronizedItemProperties, getSynchronizedStonecutterRecipes
func TestPackRecipesReachTheRecipeBookAndUpdateRecipes(t *testing.T) {
	t.Cleanup(func() { registry.ApplyPackRecipes(registry.PackRecipes{}) })
	cfg := packWorld(t)
	recipeFile(t, cfg.LevelName, "pk:clay_coal", shapedClayCoal)
	recipeFile(t, cfg.LevelName, "pk:cut", `{"type":"minecraft:stonecutting","ingredient":"minecraft:stick","result":{"id":"minecraft:diamond","count":3}}`)
	recipeFile(t, cfg.LevelName, "pk:cook", `{"type":"minecraft:smelting","ingredient":"minecraft:stick","result":"minecraft:diamond","cookingtime":10}`)
	_, c := joinPacks(t, cfg)
	c.WatchBook = true

	c.ChatCommand("reload")
	servertest.FeedbackComponent(t, c)
	c.ExpectPlay(protocol.IDPlayUpdateTags)
	update := protocol.NewReader(c.ExpectPlay(protocol.IDPlayUpdateRecipes))
	sets := map[string][]int32{}
	for range update.ReadVarInt() {
		name := update.ReadString(256)
		items := make([]int32, update.ReadVarInt())
		for i := range items {
			items[i] = update.ReadVarInt()
		}
		sets[name] = items
	}
	stick := servertest.MustItem("minecraft:stick")
	if !slices.Contains(sets["minecraft:furnace_input"], stick) {
		t.Errorf("the furnace input set holds %v, want the stick a pack recipe smelts", sets["minecraft:furnace_input"])
	}
	if got, want := update.ReadVarInt(), int32(len(registry.StonecutterRecipes)+1); got != want {
		t.Errorf("Update Recipes lists %d stonecutter recipes, want %d", got, want)
	}

	c.ExpectPlay(protocol.IDPlayRecipeBookSettings)
	book := protocol.NewReader(c.ExpectPlay(protocol.IDPlayRecipeBookAdd))
	if n := book.ReadVarInt(); n < 1 {
		t.Errorf("the reload's recipe book carries %d entries, want the player's known recipes", n)
	}

	display := registry.CraftingDisplayOfName("pk:clay_coal")
	if display == nil {
		t.Fatal("the pack recipe has no display")
	}
	got := runRecipe(c, "recipe give Testificate pk:clay_coal")
	if len(got.adds) != 1 {
		t.Fatalf("/recipe give sent %d Recipe Book Adds, want 1", len(got.adds))
	}
	add := protocol.NewReader(got.adds[0])
	if n, id := add.ReadVarInt(), add.ReadVarInt(); n != 1 || id != display.ID {
		t.Errorf("the grant was %d entries, first display %d, want 1 entry for %d", n, id, display.ID)
	}
	got = runRecipe(c, "recipe give Testificate pk:missing")
	if len(got.adds) != 0 || !got.said("recipe.notFound") {
		t.Errorf("giving a recipe no pack defines sent %d adds and chats %q, want the not-found refusal", len(got.adds), got.chats)
	}
}
