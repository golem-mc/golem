package plugins_test

import (
	"sync/atomic"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/plugin"
	"github.com/golem-mc/golem/registry"
)

// vanilla: RecipeManager.prepare, ServerPlayer.awardRecipes.
func TestPluginRecipesAddRemoveCraft(t *testing.T) {
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
		bad := plugin.NewStack(registry.ItemDiamond, 1)
		bad.Patch.Unbreakable = new(bool)
		if err := rs.Add(plugin.ShapelessRecipe{Key: plugin.Key{Namespace: "world", Path: "bad"}, Ingredients: []plugin.Ingredient{stick}, Result: bad}); err == nil {
			t.Error("a result with components was accepted")
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

// vanilla: AnvilMenu.createResult.
func TestPluginPrepareAnvilEvent(t *testing.T) {
	srv, h, run, w := inventoryPlayer(t)
	ow := srv.OverworldWorld()
	ow.Acquire(worldChunk0)
	defer ow.Release(worldChunk0)
	ow.SetBlock(4, 64, 0, registry.BlockAnvil.DefaultState())
	var seen atomic.Int32
	run(func() {
		plugin.Listen(h, func(e *plugin.PrepareItemCraftEvent) {
			seen.Add(1)
			if e.Result.Item == registry.ItemDiamondSword {
				e.Cost = 7
				e.Result.Count = 1
				name := plugin.Plain("Listener")
				e.Result.Patch.CustomName = &name
			}
		})
		testerOf(t, h).Inventory().SetItem(0, plugin.NewStack(registry.ItemDiamondSword, 1))
	})
	servertest.RunTicks(t, srv, 2)
	w.Drain()
	w.UseBlock(4, 64, 0)
	w.ExpectContainerOpen(protocol.MenuAnvil, false, 4, 64, 0)
	w.Drain()
	w.Click(30, 0, protocol.ClickModePickup)
	w.Click(0, 0, protocol.ClickModePickup)
	w.Drain()
	w.Send(protocol.IDPlayRenameItemIn, func(b *protocol.Buffer) { b.PutString("Sharp") })
	servertest.UntilTicks(t, srv, 20, "the prepare event", func() bool { return seen.Load() >= 1 })
	want := servertest.ItemStack(t, "minecraft:diamond_sword", 1)
	name := protocol.Text{Text: "Listener"}
	want.CustomName = &name
	w.AwaitWindowSlot(2, want)
	w.AwaitWindowProperty(int32(server.AnvilCostProperty), 7)
}

// vanilla: AbstractFurnaceBlockEntity.awardUsedRecipesAndPopExperience.
func TestPluginFurnaceExtractEvent(t *testing.T) {
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

// vanilla: GrindstoneMenu.createResult.
func TestPluginPrepareGrindstoneEvent(t *testing.T) {
	srv, h, run, w := inventoryPlayer(t)
	ow := srv.OverworldWorld()
	ow.Acquire(worldChunk0)
	defer ow.Release(worldChunk0)
	ow.SetBlock(4, 64, 0, registry.BlockGrindstone.DefaultState())
	var seen atomic.Int32
	run(func() {
		plugin.Listen(h, func(e *plugin.PrepareItemCraftEvent) {
			if e.Result.Item == registry.ItemDiamondSword {
				e.Result = plugin.NewStack(registry.ItemStick, 1)
				seen.Add(1)
			}
		})
		sword := plugin.NewStack(registry.ItemDiamondSword, 1)
		sword.Patch.Enchantments = map[plugin.Enchantment]int{33: 1}
		testerOf(t, h).Inventory().SetItem(0, sword)
	})
	servertest.RunTicks(t, srv, 2)
	w.Drain()
	w.UseBlock(4, 64, 0)
	w.ExpectContainerOpen(protocol.MenuGrindstone, false, 4, 64, 0)
	w.Drain()
	sword := servertest.ItemStack(t, "minecraft:diamond_sword", 1)
	sword.Enchantments = map[int32]int32{33: 1}
	w.Click(30, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{30: {}}, sword)
	w.Click(0, 0, protocol.ClickModePickup)
	// The listener's result is the one the click answers with.
	w.ExpectClickResult(map[int16]protocol.ItemStack{0: sword, 2: servertest.ItemStack(t, "minecraft:stick", 1)}, protocol.ItemStack{})
	if seen.Load() == 0 {
		t.Error("no prepare event")
	}
}
