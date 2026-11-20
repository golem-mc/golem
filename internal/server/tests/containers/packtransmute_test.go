package containers

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// craftTwo crafts the grid of two stacks in a pack of the given recipe files
// and expects the result to be what is picked up.
func craftTwo(t *testing.T, files map[string]string, a, b, want protocol.ItemStack) {
	t.Helper()
	t.Cleanup(func() { registry.ApplyPackRecipes(registry.PackRecipes{}) })
	cfg := servertest.BlockTestConfig()
	cfg.LevelName = t.TempDir()
	for name, body := range files {
		servertest.WritePackFile(t, cfg, "p", name, body)
	}
	_, w := servertest.OpenWindowed(t, cfg)
	w.SetCreativeSlot(36, a)
	w.SetCreativeSlot(37, b)
	w.PutInGrid(36, 1)
	w.PutInGrid(37, 2)
	w.Click(0, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{0: {}, 1: {}, 2: {}}, want)
}

// vanilla: TransmuteRecipe.assemble, TransmuteRecipe.createWithOriginalComponents
func TestAPackTransmuteRecipeKeepsTheInputsComponents(t *testing.T) {
	sword := withComponents(t, servertest.ItemStack(t, "minecraft:iron_sword", 1), map[string]nbt.Tag{
		"minecraft:custom_name": nbt.String("Edge"),
	})
	sword.Enchantments = map[int32]int32{servertest.SharpnessID(t): 3}
	sword.Damage = 5
	want := sword
	want.ItemID = servertest.ItemStack(t, "minecraft:diamond_sword", 1).ItemID
	craftTwo(t, map[string]string{"pk/recipe/upgrade.json": `{"type":"minecraft:crafting_transmute","input":"minecraft:iron_sword",
		"material":"minecraft:diamond","result":{"id":"minecraft:diamond_sword"}}`},
		sword, servertest.ItemStack(t, "minecraft:diamond", 1), want)
}

// vanilla: RecipeManager.prepare, DyeRecipe.assemble
func TestAPackDyeRecipeDyesItsOwnTarget(t *testing.T) {
	craftTwo(t, map[string]string{"pk/recipe/dyed.json": `{"type":"minecraft:crafting_dye","target":"minecraft:leather_helmet",
		"dye":"#minecraft:dyes","result":{"id":"minecraft:leather_chestplate"}}`},
		servertest.ItemStack(t, "minecraft:leather_helmet", 1), servertest.ItemStack(t, "minecraft:red_dye", 1),
		dyedStack(t, "minecraft:leather_chestplate", 0xB02E26))
}

// A pack file of a vanilla recipe's ID replaces it.
// vanilla: RecipeManager.prepare
func TestAPackTransmuteRecipeReplacesAVanillaOne(t *testing.T) {
	box := servertest.ItemStack(t, "minecraft:shulker_box", 1)
	craftTwo(t, map[string]string{"minecraft/recipe/red_shulker_box.json": `{"type":"minecraft:crafting_transmute",
		"input":"#minecraft:shulker_boxes","material":"minecraft:red_dye","result":{"id":"minecraft:lime_shulker_box"}}`},
		box, servertest.ItemStack(t, "minecraft:red_dye", 1), servertest.ItemStack(t, "minecraft:lime_shulker_box", 1))
}
