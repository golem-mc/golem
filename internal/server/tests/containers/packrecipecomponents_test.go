package containers

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// withComponents returns stack with the named components read onto it.
func withComponents(t *testing.T, stack protocol.ItemStack, components map[string]nbt.Tag) protocol.ItemStack {
	t.Helper()
	for name, value := range components {
		if !registry.ReadComponent(&stack, name, value) {
			t.Fatalf("component %s is refused", name)
		}
	}
	return stack
}

// writePack writes the files of a data pack named p under the level.
func writePack(t *testing.T, level string, files map[string]string) {
	t.Helper()
	files["pack.mcmeta"] = `{"pack":{"description":"p"}}`
	for file, body := range files {
		path := filepath.Join(level, "datapacks", "p", file)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// vanilla: ItemStackTemplate.CODEC, ShapedRecipe.assemble
func TestAPackRecipeResultKeepsItsComponents(t *testing.T) {
	t.Cleanup(func() { registry.ApplyPackRecipes(registry.PackRecipes{}) })
	cfg := servertest.BlockTestConfig()
	cfg.LevelName = t.TempDir()
	writePack(t, cfg.LevelName, map[string]string{
		"data/pk/recipe/wand.json": `{"type":"minecraft:crafting_shaped","pattern":["#"],
			"key":{"#":"minecraft:cobblestone"},
			"result":{"id":"minecraft:stick","components":{"minecraft:custom_name":"Wand","minecraft:enchantment_glint_override":true}}}`,
	})
	_, w := servertest.OpenWindowed(t, cfg)
	w.SetCreativeSlot(36, servertest.ItemStack(t, "minecraft:crafting_table", 1))
	w.PlaceBlock(4, 63, 0, protocol.FaceTop)
	w.ExpectBlockAck()
	w.UseBlock(4, 64, 0)
	w.ExpectContainerOpen(protocol.MenuCrafting, false, 0, 0, 0)
	w.SetCreativeSlot(36, servertest.ItemStack(t, "minecraft:cobblestone", 1))
	w.PutInGrid(37, 1)
	w.Click(0, 0, protocol.ClickModePickup)
	wand := withComponents(t, servertest.ItemStack(t, "minecraft:stick", 1), map[string]nbt.Tag{
		"minecraft:custom_name":                nbt.String("Wand"),
		"minecraft:enchantment_glint_override": nbt.Byte(1),
	})
	w.ExpectClickResult(map[int16]protocol.ItemStack{
		0: {}, 1: {}, 2: {}, 3: {}, 4: {}, 5: {}, 6: {}, 7: {}, 8: {}, 9: {},
	}, wand)
}

// vanilla: AbstractCookingRecipe.assemble, AbstractFurnaceBlockEntity.burn
func TestAPackSmeltingResultKeepsItsComponents(t *testing.T) {
	t.Cleanup(func() { registry.ApplyPackRecipes(registry.PackRecipes{}) })
	cfg := servertest.BlockTestConfig()
	cfg.LevelName = t.TempDir()
	writePack(t, cfg.LevelName, map[string]string{
		"data/pk/recipe/tagged.json": `{"type":"minecraft:smelting","ingredient":"minecraft:feather","cookingtime":200,
			"result":{"id":"minecraft:stick","components":{"minecraft:custom_data":{"wand":1}}}}`,
	})
	srv, _ := servertest.OpenWindowed(t, cfg)
	if !srv.PlaceContainer(4, 64, 0, "minecraft:furnace") {
		t.Fatal("could not place the furnace")
	}
	for slot, stack := range map[int]protocol.ItemStack{
		0: servertest.ItemStack(t, "minecraft:feather", 1),
		1: servertest.ItemStack(t, "minecraft:coal", 1),
	} {
		if !srv.SetInventoryAt(4, 64, 0, slot, stack) {
			t.Fatalf("could not fill furnace slot %d", slot)
		}
	}
	servertest.HoldTicks(t, srv)
	servertest.RunTicks(t, srv, 201)
	want := withComponents(t, servertest.ItemStack(t, "minecraft:stick", 1), map[string]nbt.Tag{
		"minecraft:custom_data": nbt.Compound{"wand": nbt.Int(1)},
	})
	if got := srv.InventoryAt(4, 64, 0); len(got) != 3 || !got[2].Equal(want) {
		t.Errorf("the furnace holds %+v, want the tagged stick in the output", got)
	}
}
