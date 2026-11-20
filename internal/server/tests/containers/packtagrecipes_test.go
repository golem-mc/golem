package containers

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// openCraftingWithItemTag starts a server whose pack writes one item tag and
// opens a crafting table for the player.
// vanilla: Ingredient, ReloadableServerResources.updateStaticRegistryTags
func openCraftingWithItemTag(t *testing.T, tag, body string) *servertest.WindowedClient {
	t.Helper()
	t.Cleanup(func() {
		registry.ApplyPackTags(nil)
		registry.ApplyPackRecipes(registry.PackRecipes{})
	})
	cfg := servertest.BlockTestConfig()
	cfg.LevelName = t.TempDir()
	pack := filepath.Join(cfg.LevelName, "datapacks", "p")
	for file, content := range map[string]string{
		"pack.mcmeta": `{"pack":{"description":"p"}}`,
		"data/minecraft/tags/item/" + tag + ".json": body,
	} {
		path := filepath.Join(pack, file)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	_, w := servertest.OpenWindowed(t, cfg)
	w.SetCreativeSlot(36, servertest.ItemStack(t, "minecraft:crafting_table", 1))
	w.PlaceBlock(4, 63, 0, protocol.FaceTop)
	w.ExpectBlockAck()
	w.UseBlock(4, 64, 0)
	w.ExpectContainerOpen(protocol.MenuCrafting, false, 0, 0, 0)
	return w
}

func TestAPackTagAddsAnItemToABuiltInRecipe(t *testing.T) {
	w := openCraftingWithItemTag(t, "stone_tool_materials", `{"values":["minecraft:deepslate"]}`)
	w.SetCreativeSlot(36, servertest.ItemStack(t, "minecraft:deepslate", 3))
	w.SetCreativeSlot(37, servertest.ItemStack(t, "minecraft:stick", 2))
	w.PutInGrid(37, 1, 2, 3)
	w.PutInGrid(38, 5, 8)
	w.Click(0, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{
		0: {}, 1: {}, 2: {}, 3: {}, 4: {}, 5: {}, 6: {}, 7: {}, 8: {}, 9: {},
	}, servertest.ItemStack(t, "minecraft:stone_pickaxe", 1))
}

func TestAPackTagTakesAnItemOutOfABuiltInRecipe(t *testing.T) {
	w := openCraftingWithItemTag(t, "planks", `{"replace":true,"values":["minecraft:oak_planks"]}`)
	w.SetCreativeSlot(36, servertest.ItemStack(t, "minecraft:birch_planks", 2))
	// Birch planks no longer make sticks.
	w.Click(37, 0, protocol.ClickModePickup)
	w.SettleTrackingState()
	for _, cell := range []int16{1, 4} {
		w.Click(cell, 1, protocol.ClickModePickup)
		w.SettleAndCollect(func(id int32, data []byte) {
			if id != protocol.IDPlayContainerSlot {
				w.Record(id, data)
				return
			}
			r := protocol.NewReader(data)
			r.ReadVarInt()
			w.TrackState(w.Window, r.ReadVarInt())
			// One plank makes a button, which names birch planks itself.
			if slot := r.ReadI16(); slot == 0 && cell == 4 {
				if stack, _ := r.ReadSlot(); !stack.Empty() {
					t.Errorf("the grid of birch planks has the result %+v", stack)
				}
			}
		})
	}
}
