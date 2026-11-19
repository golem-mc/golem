package containers

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// vanilla: BrewingRecipe.MAP_CODEC, BrewingStandBlockEntity.doBrew
func TestAPackAddsAndOverridesBrewingRecipes(t *testing.T) {
	t.Cleanup(func() { registry.ApplyPackRecipes(registry.PackRecipes{}) })
	cfg := servertest.BlockTestConfig()
	cfg.LevelName = t.TempDir()
	pack := filepath.Join(cfg.LevelName, "datapacks", "p")
	// Water and sugar already make mundane in vanilla. Replace that competing
	// mix so pk:brewing/sugar_water can be the first matching recipe.
	for file, body := range map[string]string{
		"data/minecraft/recipe/brewing/potion_water_sugar.json": `{"type":"minecraft:brewing",
			"input":{"item":"minecraft:brick"},"reagent":{"item":"minecraft:stick"},
			"output":{"id":"minecraft:glass_bottle"}}`,
		"pack.mcmeta": `{"pack":{"description":"p"}}`,
		"data/pk/recipe/brewing/sugar_water.json": `{"type":"minecraft:brewing",
			"input":{"item":"minecraft:potion","potion_contents":{"potions":"minecraft:water"}},
			"reagent":{"item":"minecraft:sugar"},
			"output":{"id":"minecraft:potion","components":{"minecraft:potion_contents":{"potion":"minecraft:swiftness"}}}}`,
		"data/minecraft/recipe/brewing/potion_awkward_sugar.json": `{"type":"minecraft:brewing",
			"input":{"item":"minecraft:potion","potion_contents":{"potions":"minecraft:awkward"}},
			"reagent":{"item":"minecraft:sugar"},
			"output":{"id":"minecraft:potion","components":{"minecraft:potion_contents":{"potion":"minecraft:strength"}}}}`,
	} {
		path := filepath.Join(pack, file)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	srv, w := servertest.OpenWindowed(t, cfg)
	placeBrewingStand(t, srv, w)
	if !srv.PlaceContainer(6, 64, 0, "minecraft:brewing_stand") {
		t.Fatal("could not place the second brewing stand")
	}
	for _, stand := range []struct {
		x      int32
		potion string
	}{{4, "water"}, {6, "awkward"}} {
		for slot, stack := range map[int]protocol.ItemStack{
			0: servertest.PotionBottle(t, "minecraft:potion", "minecraft:"+stand.potion),
			3: servertest.ItemStack(t, "minecraft:sugar", 1),
			4: servertest.ItemStack(t, "minecraft:blaze_powder", 1),
		} {
			if !srv.SetInventoryAt(stand.x, 64, 0, slot, stack) {
				t.Fatalf("could not fill stand %d slot %d", stand.x, slot)
			}
		}
	}
	servertest.HoldTicks(t, srv)
	servertest.RunTicks(t, srv, 1)
	for _, x := range []int32{4, 6} {
		if remaining, _, _ := srv.BrewingAt(x, 64, 0); remaining != 400 {
			t.Errorf("stand %d starts with %d ticks remaining, want 400", x, remaining)
		}
	}
	servertest.RunTicks(t, srv, 400)
	for _, stand := range []struct {
		x      int32
		potion string
	}{{4, "swiftness"}, {6, "strength"}} {
		got := srv.InventoryAt(stand.x, 64, 0)
		want := servertest.PotionBottle(t, "minecraft:potion", "minecraft:"+stand.potion)
		if len(got) != 5 || !got[0].Equal(want) || !got[3].Empty() {
			t.Errorf("stand %d holds %+v, want %s and spent sugar", stand.x, got, stand.potion)
		}
	}
	// Shift-click the replacement mix's new input and reagent through the
	// menu: both property sets must include the loaded recipe's items.
	if !srv.SetInventoryAt(4, 64, 0, 0, protocol.ItemStack{}) {
		t.Fatal("could not empty the first stand")
	}
	w.UseBlock(4, 64, 0)
	w.ExpectContainerOpenType(protocol.MenuBrewingStand, protocol.BlockEntityBrewingStand, 4, 64, 0)
	for _, entry := range []struct {
		item string
		slot int16
	}{{"minecraft:brick", 0}, {"minecraft:stick", 3}} {
		stack := servertest.ItemStack(t, entry.item, 1)
		w.SetCreativeSlot(36, stack)
		w.Click(32, 0, protocol.ClickModeQuickMove)
		w.ExpectClickResult(map[int16]protocol.ItemStack{entry.slot: stack, 32: {}}, protocol.ItemStack{})
	}
	w.CloseWindow()
	w.ExpectWindowZero()
	servertest.RunTicks(t, srv, 401)
	got := srv.InventoryAt(4, 64, 0)
	want := servertest.ItemStack(t, "minecraft:glass_bottle", 1)
	if len(got) != 5 || !got[0].Equal(want) || !got[3].Empty() {
		t.Error("the wildcard input did not brew a fresh glass bottle without potion data")
	}
}
