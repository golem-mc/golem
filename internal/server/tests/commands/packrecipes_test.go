package commands

import (
	"os"
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

// TestPackRecipesCraftOverrideAndSkip checks that a data pack's shaped recipe
// crafts in the player's grid, that a pack recipe of a built-in recipe's ID
// replaces it (the built-in one no longer crafts) while the other built-ins
// stay, and that a recipe file that does not decode adds nothing.
// vanilla: RecipeManager.prepare
func TestPackRecipesCraftOverrideAndSkip(t *testing.T) {
	t.Cleanup(func() { registry.ApplyPackRecipes(registry.PackRecipes{}) })
	cfg := packWorld(t)
	recipeFile(t, cfg.LevelName, "pk:clay_coal", shapedClayCoal)
	recipeFile(t, cfg.LevelName, "minecraft:oak_planks", clayToPlanks)
	recipeFile(t, cfg.LevelName, "minecraft:birch_planks", `{"type":"minecraft:crafting_shapeless","ingredients":[`)
	recipeFile(t, cfg.LevelName, "pk:broken", `{"type":"minecraft:crafting_shaped","pattern":["a"],`)
	recipeFile(t, cfg.LevelName, "pk:no_item", `{"type":"minecraft:crafting_shapeless","ingredients":["minecraft:gold_nugget"],"result":"minecraft:not_an_item"}`)
	srv, w := servertest.OpenWindowedOps(t, cfg)
	for slot, item := range map[int]string{36: "minecraft:clay_ball", 37: "minecraft:coal", 38: "minecraft:oak_log", 39: "minecraft:birch_log", 40: "minecraft:gold_nugget", 41: "minecraft:jungle_log"} {
		srv.GiveSlot("Testificate", slot, servertest.ItemStack(t, item, 8))
	}

	expectCraft(t, "clay and coal", gridResult(t, w, placement{36, 1}, placement{37, 2}), "minecraft:diamond", 2)
	expectCraft(t, "clay alone", gridResult(t, w, placement{36, 1}), "minecraft:oak_planks", 7)
	if got := gridResult(t, w, placement{38, 1}); !got.Empty() {
		t.Errorf("an oak log crafts %+v although a pack replaced the oak planks recipe", got)
	}
	if got := gridResult(t, w, placement{39, 1}); !got.Empty() {
		t.Errorf("a birch log crafts %+v although its recipe file does not decode", got)
	}
	expectCraft(t, "a jungle log", gridResult(t, w, placement{41, 1}), "minecraft:jungle_planks", 4)
	if got := gridResult(t, w, placement{40, 1}); !got.Empty() {
		t.Errorf("a gold nugget crafts %+v from a recipe that does not decode", got)
	}
}

// TestReloadPicksUpPackRecipes checks that /reload reads a changed recipe
// file, a removed one gives the built-in recipe back, and disabling the pack
// with /datapack drops its recipes.
// vanilla: MinecraftServer.reloadResources
func TestReloadPicksUpPackRecipes(t *testing.T) {
	t.Cleanup(func() { registry.ApplyPackRecipes(registry.PackRecipes{}) })
	cfg := packWorld(t)
	recipeFile(t, cfg.LevelName, "pk:clay_coal", shapedClayCoal)
	planks := recipeFile(t, cfg.LevelName, "minecraft:oak_planks", clayToPlanks)
	srv, w := servertest.OpenWindowedOps(t, cfg)
	for slot, item := range map[int]string{36: "minecraft:clay_ball", 37: "minecraft:coal", 38: "minecraft:oak_log"} {
		srv.GiveSlot("Testificate", slot, servertest.ItemStack(t, item, 8))
	}
	reload := func(command string) {
		t.Helper()
		w.ChatCommand(command)
		w.SettleAndCollect(func(id int32, data []byte) { w.Record(id, data) })
	}
	craftClayCoal := func() protocol.ItemStack { return gridResult(t, w, placement{36, 1}, placement{37, 2}) }

	expectCraft(t, "before the reload", craftClayCoal(), "minecraft:diamond", 2)
	recipeFile(t, cfg.LevelName, "pk:clay_coal", `{"type":"minecraft:crafting_shapeless","ingredients":["minecraft:clay_ball","minecraft:coal"],`+
		`"result":{"id":"minecraft:emerald","count":5}}`)
	reload("reload")
	expectCraft(t, "after the reload changed the file", craftClayCoal(), "minecraft:emerald", 5)

	if err := os.Remove(planks); err != nil {
		t.Fatal(err)
	}
	reload("reload")
	expectCraft(t, "the oak planks recipe after its file was removed", gridResult(t, w, placement{38, 1}), "minecraft:oak_planks", 4)
	if got := gridResult(t, w, placement{36, 1}); !got.Empty() {
		t.Errorf("clay crafts %+v after the oak planks file was removed", got)
	}

	reload(`datapack disable "file/a"`)
	if got := craftClayCoal(); !got.Empty() {
		t.Errorf("clay and coal craft %+v with the pack disabled", got)
	}
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

// furnaceEntry is a Recipe Book Add entry of a cooking recipe, as read off
// the wire.
type furnaceEntry struct {
	id, displayType     int32
	ingredient          []int32
	result, count       int32
	station, duration   int32
	experience          float32
	group, category     int32
	requirements        []int32
	notify, highlighted bool
}

// readFurnaceEntry reads one cooking recipe's entry whose ingredient is an
// item list: display ID, FurnaceRecipeDisplay, group, category, requirements
// and flags. vanilla: ClientboundRecipeBookAddPacket's Entry
func readFurnaceEntry(t *testing.T, r *protocol.Reader) furnaceEntry {
	t.Helper()
	itemList := func(what string) []int32 {
		n := r.ReadVarInt()
		if n == 0 {
			t.Fatalf("the %s is the tag %s, want an item list", what, r.ReadString(32767))
		}
		items := make([]int32, n-1)
		for i := range items {
			items[i] = r.ReadVarInt()
		}
		return items
	}
	var e furnaceEntry
	e.id, e.displayType = r.ReadVarInt(), r.ReadVarInt()
	if typ := r.ReadVarInt(); typ != 6 {
		t.Fatalf("ingredient slot display type %d, want 6 (a HolderSet)", typ)
	}
	e.ingredient = itemList("ingredient")
	if fuel, stack := r.ReadVarInt(), r.ReadVarInt(); fuel != 1 || stack != 5 {
		t.Fatalf("fuel and result slot display types %d and %d, want 1 (any fuel) and 5 (an item stack)", fuel, stack)
	}
	e.result, e.count = r.ReadVarInt(), r.ReadVarInt()
	if added, removed := r.ReadVarInt(), r.ReadVarInt(); added != 0 || removed != 0 {
		t.Fatalf("the result carries %d added and %d removed components, want none", added, removed)
	}
	if typ := r.ReadVarInt(); typ != 4 {
		t.Fatalf("station slot display type %d, want 4 (an item)", typ)
	}
	e.station, e.duration, e.experience = r.ReadVarInt(), r.ReadVarInt(), r.ReadF32()
	e.group, e.category = r.ReadVarInt(), r.ReadVarInt()
	if !r.ReadBool() || r.ReadVarInt() != 1 {
		t.Fatal("the entry's requirements are not one square")
	}
	e.requirements = itemList("requirement")
	flags := r.ReadU8()
	e.notify, e.highlighted = flags&1 != 0, flags&2 != 0
	if err := r.Err(); err != nil {
		t.Fatal(err)
	}
	return e
}

// grantFurnaceEntry runs /recipe give for one recipe and reads its entry.
func grantFurnaceEntry(t *testing.T, c *servertest.Client, name string) furnaceEntry {
	t.Helper()
	got := runRecipe(c, "recipe give Testificate "+name)
	if len(got.adds) != 1 {
		t.Fatalf("/recipe give %s sent %d Recipe Book Adds, want 1", name, len(got.adds))
	}
	add := protocol.NewReader(got.adds[0])
	if n := add.ReadVarInt(); n != 1 {
		t.Fatalf("/recipe give %s added %d entries, want 1", name, n)
	}
	return readFurnaceEntry(t, add)
}

// TestPackCookingRecipesReachTheFurnaceBook checks that a data pack's new
// smelting recipe is granted with a furnace display built from its file - the
// item list in file order, its category, group, result, time and experience -
// and that a pack recipe replacing a built-in cooking recipe keeps that
// recipe's display ID and shows the pack's ingredient.
// vanilla: AbstractCookingRecipe.display, SmeltingRecipe.recipeBookCategory
func TestPackCookingRecipesReachTheFurnaceBook(t *testing.T) {
	t.Cleanup(func() { registry.ApplyPackRecipes(registry.PackRecipes{}) })
	stick, apple := servertest.MustItem("minecraft:stick"), servertest.MustItem("minecraft:apple")
	if stick < apple {
		t.Fatal("the stick sorts before the apple: list them the other way round so file order differs from ID order")
	}
	potato := slices.IndexFunc(registry.FurnaceDisplays, func(d registry.FurnaceDisplay) bool { return d.Name == "minecraft:baked_potato" })
	cfg := packWorld(t)
	recipeFile(t, cfg.LevelName, "pk:cook", `{"type":"minecraft:smelting","category":"food","group":"pk",`+
		`"ingredient":["minecraft:stick","minecraft:apple"],"result":{"id":"minecraft:diamond","count":2},"experience":0.5,"cookingtime":10}`)
	recipeFile(t, cfg.LevelName, "minecraft:baked_potato",
		`{"type":"minecraft:smelting","category":"blocks","ingredient":["minecraft:clay_ball"],"result":"minecraft:brick","cookingtime":30}`)
	_, c := joinPacks(t, cfg)
	c.WatchBook = true

	everyKind := int32(len(registry.CraftingDisplays) + len(registry.FurnaceDisplays) + len(registry.StonecutterRecipes) +
		len(registry.SmithingTransformRecipes) + len(registry.SmithingTrimRecipes))
	cook := grantFurnaceEntry(t, c, "pk:cook")
	diamond, furnace := servertest.MustItem("minecraft:diamond"), servertest.MustItem("minecraft:furnace")
	// furnace_food is recipe_book_category 4 (RecipeBookCategories).
	if cook.id < everyKind || cook.displayType != 2 || cook.result != diamond || cook.count != 2 || cook.station != furnace ||
		cook.duration != 10 || cook.experience != 0.5 || cook.group == 0 || cook.category != 4 || !cook.notify {
		t.Errorf("pk:cook's entry = %+v, want a new furnace display (ID %d up) of 2 diamonds in 10 ticks for 0.5 xp, grouped, in furnace_food, notifying",
			cook, everyKind)
	}
	if want := []int32{stick, apple}; !slices.Equal(cook.ingredient, want) || !slices.Equal(cook.requirements, want) {
		t.Errorf("pk:cook's ingredient is %v and requirement %v, want the file's order %v", cook.ingredient, cook.requirements, want)
	}

	baked := grantFurnaceEntry(t, c, "minecraft:baked_potato")
	clay, brick := servertest.MustItem("minecraft:clay_ball"), servertest.MustItem("minecraft:brick")
	// furnace_blocks is recipe_book_category 5.
	if baked.id != registry.FurnaceDisplays[potato].ID || !slices.Equal(baked.ingredient, []int32{clay}) ||
		baked.result != brick || baked.duration != 30 || baked.group != 0 || baked.category != 5 {
		t.Errorf("the replaced baked_potato's entry = %+v, want display %d smelting clay into a brick in furnace_blocks",
			baked, registry.FurnaceDisplays[potato].ID)
	}
}

// TestPackRecipeJoinsABuiltinRecipeGroup checks that a pack recipe naming a
// built-in recipe group is drawn under that group's ID, not a new one.
// vanilla: RecipeManager.unpackRecipeInfo
func TestPackRecipeJoinsABuiltinRecipeGroup(t *testing.T) {
	t.Cleanup(func() { registry.ApplyPackRecipes(registry.PackRecipes{}) })
	want := slices.Index(registry.RecipeGroupNames, "iron_ingot")
	if want < 0 {
		t.Fatal("no built-in iron_ingot group")
	}
	cfg := packWorld(t)
	recipeFile(t, cfg.LevelName, "pk:iron", `{"type":"minecraft:smelting","category":"misc","group":"iron_ingot",`+
		`"ingredient":"minecraft:stick","result":{"id":"minecraft:diamond"},"experience":0,"cookingtime":10}`)
	_, c := joinPacks(t, cfg)
	c.WatchBook = true
	if got := grantFurnaceEntry(t, c, "pk:iron"); // The wire's group is the ID plus one; 0 names no group.
	got.group != int32(want)+1 {
		t.Errorf("pk:iron's group = %d, want the built-in iron_ingot group %d", got.group, want+1)
	}
}
