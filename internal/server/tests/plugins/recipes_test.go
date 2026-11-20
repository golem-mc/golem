package plugins_test

import (
	"sync/atomic"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/plugin"
	"github.com/golem-mc/golem/registry"
)

// vanilla: RecipeManager.prepare, ServerPlayer.awardRecipes.
func TestPluginRecipesAddRemoveCraft(t *testing.T) {
	t.Parallel()
	_, h, run, w := inventoryPlayer(t)
	// The recipe set is process-wide.
	t.Cleanup(func() { registry.ApplyPackRecipes(registry.PackRecipes{}) })
	stick := plugin.Ingredient{Items: []registry.Item{registry.ItemStick}}
	key := plugin.Key{Namespace: "world", Path: "stick_gem"}
	run(func() {
		rs := h.Recipes()
		if err := rs.Add(plugin.ShapedRecipe{Key: plugin.MinecraftKey("x"), Pattern: []string{"S"}, Keys: map[rune]plugin.Ingredient{'S': stick},
			Result: plugin.NewStack(registry.ItemDiamond, 1)}); err == nil {
			t.Error("a recipe outside the plugin's namespace was accepted")
		}
		if err := rs.Add(plugin.ShapedRecipe{Key: key, Pattern: []string{"SS", "SS"}, Keys: map[rune]plugin.Ingredient{'S': stick},
			Result: plugin.NewStack(registry.ItemDiamond, 2)}); err != nil {
			t.Fatal(err)
		}
		if rs.Add(plugin.ShapedRecipe{Key: key, Pattern: []string{"S"}, Keys: map[rune]plugin.Ingredient{'S': stick},
			Result: plugin.NewStack(registry.ItemDiamond, 1)}) == nil {
			t.Error("a repeated key was accepted")
		}
		got, ok := rs.Get(key)
		if !ok || got.RecipeKey() != key {
			t.Errorf("Get = %v, %v", got, ok)
		}
		if _, ok := rs.Get(plugin.MinecraftKey("crafting_table")); !ok {
			t.Error("a built-in recipe is missing from Get")
		}
		testerOf(t, h).Inventory().SetItem(0, plugin.NewStack(registry.ItemStick, 4))
	})
	w.Drain()
	w.PutInGrid(36, 1, 2, 3, 4)
	w.Click(0, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{
		0: {}, 1: {}, 2: {}, 3: {}, 4: {},
	}, servertest.ItemStack(t, "minecraft:diamond", 2))

	run(func() {
		rs := h.Recipes()
		if !rs.Remove(key) || rs.Remove(key) {
			t.Error("Remove of an added recipe")
		}
		if !rs.Remove(plugin.MinecraftKey("crafting_table")) {
			t.Error("Remove of a built-in recipe")
		}
		if _, ok := rs.Get(plugin.MinecraftKey("crafting_table")); ok {
			t.Error("a removed built-in recipe is still listed")
		}
	})
}

// vanilla: InventoryMenu.slotsChanged, ResultSlot.onTake.
func TestPluginPrepareAndCraftEvents(t *testing.T) {
	t.Parallel()
	srv, h, run, w := inventoryPlayer(t)
	var prepared int
	var crafted atomic.Int32
	cancel := true
	var lastPrep *plugin.PrepareItemCraftEvent
	var lastCraft *plugin.CraftItemEvent
	run(func() {
		plugin.Listen(h, func(e *plugin.PrepareItemCraftEvent) {
			prepared++
			lastPrep = e
			if e.Result.Item == registry.ItemHoneyBlock {
				e.Result = plugin.NewStack(registry.ItemDiamond, 3)
			}
		})
		plugin.Listen(h, func(e *plugin.CraftItemEvent) {
			crafted.Add(1)
			lastCraft = e
			e.SetCancelled(cancel)
		})
		testerOf(t, h).Inventory().SetItem(0, plugin.NewStack(registry.ItemHoneyBottle, 4))
	})
	w.Drain()
	w.PutInGrid(36, 1, 2, 3, 4)
	w.Drain()
	run(func() {
		if prepared == 0 || lastPrep.Recipe == nil || lastPrep.Recipe.RecipeKey() != plugin.MinecraftKey("honey_block") {
			t.Errorf("prepare event %+v after %d", lastPrep, prepared)
		}
	})
	w.Click(0, 0, protocol.ClickModePickup)
	servertest.UntilTicks(t, srv, 20, "the craft event", func() bool { return crafted.Load() == 1 })
	w.ExpectWindowContent()
	run(func() {
		if lastCraft.Result.Item != registry.ItemDiamond || lastCraft.Result.Count != 3 || lastCraft.Recipe == nil {
			t.Errorf("craft event %+v", lastCraft)
		}
		if testerOf(t, h).Cursor().Count != 0 {
			t.Error("a cancelled craft gave the result")
		}
	})
	cancel = false
	w.Click(0, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{0: {}, 1: servertest.ItemStack(t, "minecraft:glass_bottle", 1),
		2: servertest.ItemStack(t, "minecraft:glass_bottle", 1), 3: servertest.ItemStack(t, "minecraft:glass_bottle", 1),
		4: servertest.ItemStack(t, "minecraft:glass_bottle", 1)}, servertest.ItemStack(t, "minecraft:diamond", 3))
}

// vanilla: AbstractFurnaceBlockEntity.awardUsedRecipesAndPopExperience.
func TestPluginFurnaceExtractEvent(t *testing.T) {
	t.Parallel()
	srv, h, run, w := inventoryPlayer(t)
	ow := srv.OverworldWorld()
	ow.Acquire(worldChunk0)
	defer ow.Release(worldChunk0)
	be := placeContainerEntity(ow, 4, 64, 0, registry.BlockFurnace)
	recipe := registry.SmeltingRecipes[0]
	var seen atomic.Int32
	var got *plugin.FurnaceExtractEvent
	run(func() {
		plugin.Listen(h, func(e *plugin.FurnaceExtractEvent) {
			got = e
			e.Experience = 0
			seen.Add(1)
		})
		containerHandle(t, h.Overworld(), plugin.BlockPos{X: 4, Y: 64}).Inventory().SetItem(2, plugin.NewStack(registry.Item(recipe.ResultID), 2))
		be.Use(func(b *world.BlockEntity) { b.RecordRecipeUsed(recipe.ID); b.RecordRecipeUsed(recipe.ID) })
	})
	servertest.RunTicks(t, srv, 2)
	w.Drain()
	w.UseBlock(4, 64, 0)
	w.ExpectContainerOpenType(protocol.MenuFurnace, protocol.BlockEntityFurnace, 4, 64, 0)
	w.Click(2, 0, protocol.ClickModePickup)
	servertest.UntilTicks(t, srv, 20, "the extract event", func() bool { return seen.Load() == 1 })
	run(func() {
		if got.Item.Item != registry.Item(recipe.ResultID) || got.Item.Count != 2*int(recipe.ResultCount) || got.Block.Pos.X != 4 {
			t.Errorf("extract event %+v", got)
		}
	})
}

// vanilla: ItemStackTemplate.apply, ShapelessRecipe.assemble
func TestPluginRecipeResultKeepsItsComponents(t *testing.T) {
	// Not parallel: the recipe set is process-wide.
	_, h, run, w := inventoryPlayer(t)
	t.Cleanup(func() { registry.ApplyPackRecipes(registry.PackRecipes{}) })
	run(func() {
		result := plugin.NewStack(registry.ItemDiamond, 1)
		yes := true
		result.Patch.Unbreakable = &yes
		if err := h.Recipes().Add(plugin.ShapelessRecipe{Key: plugin.Key{Namespace: "world", Path: "tough"},
			Ingredients: []plugin.Ingredient{{Items: []registry.Item{registry.ItemStick}}}, Result: result}); err != nil {
			t.Fatal(err)
		}
		testerOf(t, h).Inventory().SetItem(0, plugin.NewStack(registry.ItemStick, 1))
	})
	w.Drain()
	w.PutInGrid(36, 1)
	w.Click(0, 0, protocol.ClickModePickup)
	want := servertest.ItemStack(t, "minecraft:diamond", 1)
	want.Unbreakable = true
	w.ExpectClickResult(map[int16]protocol.ItemStack{0: {}, 1: {}, 2: {}, 3: {}, 4: {}}, want)
}
