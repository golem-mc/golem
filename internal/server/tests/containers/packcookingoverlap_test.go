package containers

import (
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// overlapPack writes two recipes of kind taking bone, the first making a stick
// and the second, whose other input is string, a bowl.
func overlapPack(t *testing.T, cfg *config.Config, kind string) {
	t.Helper()
	servertest.WritePackFile(t, cfg, "p", "pk/recipe/a.json", `{"type":"minecraft:`+kind+`","ingredient":["minecraft:feather","minecraft:bone"],"cookingtime":10,"result":{"id":"minecraft:stick"}}`)
	servertest.WritePackFile(t, cfg, "p", "pk/recipe/b.json", `{"type":"minecraft:`+kind+`","ingredient":["minecraft:bone","minecraft:string"],"cookingtime":10,"result":{"id":"minecraft:bowl"}}`)
}

// furnaceMakes cooks the inputs one after another in a furnace and returns
// the item its output slot ends with.
func furnaceMakes(t *testing.T, inputs ...string) int32 {
	t.Cleanup(func() { registry.ApplyPackRecipes(registry.PackRecipes{}) })
	cfg := servertest.BlockTestConfig()
	cfg.LevelName = t.TempDir()
	overlapPack(t, cfg, "smelting")
	srv, _ := servertest.OpenWindowed(t, cfg)
	if !srv.PlaceContainer(4, 64, 0, "minecraft:furnace") {
		t.Fatal("could not place the furnace")
	}
	srv.SetInventoryAt(4, 64, 0, 1, servertest.ItemStack(t, "minecraft:coal", 1))
	for _, in := range inputs {
		srv.SetInventoryAt(4, 64, 0, 0, servertest.ItemStack(t, in, 1))
	}
	servertest.HoldTicks(t, srv)
	servertest.RunTicks(t, srv, 12)
	got := srv.InventoryAt(4, 64, 0)
	if len(got) != 3 || got[2].Empty() {
		t.Fatalf("the furnace holds %+v, want an output", got)
	}
	return got[2].ItemID
}

// vanilla: RecipeManager.createCheck, AbstractFurnaceBlockEntity.setItem
func TestAFurnaceKeepsItsLastMatchingRecipeForOverlappingInputs(t *testing.T) {
	stick := servertest.ItemStack(t, "minecraft:stick", 1).ItemID
	bowl := servertest.ItemStack(t, "minecraft:bowl", 1).ItemID
	if got := furnaceMakes(t, "minecraft:bone"); got != stick {
		t.Errorf("an unprimed furnace made item %d, want the first recipe's stick", got)
	}
	if got := furnaceMakes(t, "minecraft:string", "minecraft:bone"); got != bowl {
		t.Errorf("a primed furnace made item %d, want the later recipe's bowl", got)
	}
}

// campfireDrops cooks the foods one after another on a lit campfire and
// returns how many sticks and bowls lie on the ground.
func campfireDrops(t *testing.T, foods ...string) (sticks, bowls int) {
	t.Cleanup(func() { registry.ApplyPackRecipes(registry.PackRecipes{}) })
	cfg := servertest.SurvivalTestConfig()
	cfg.LevelName = t.TempDir()
	overlapPack(t, cfg, "campfire_cooking")
	srv, w := servertest.OpenWindowed(t, cfg)
	srv.StopTick()
	state, ok := registry.FindState("minecraft:campfire", map[string]string{"lit": "true"})
	if !ok {
		t.Fatal("no lit campfire state")
	}
	srv.SetBlock(3, 64, 0, state)
	for _, food := range foods {
		srv.GiveSlot("Testificate", player.HotbarSlot, servertest.ItemStack(t, food, 1))
		w.Settle()
		w.UseBlock(3, 64, 0)
		w.Settle()
		servertest.TickBlockEntitiesByHand(srv, 10)
	}
	for _, it := range srv.ItemsInDimension("minecraft:overworld") {
		switch it.Item {
		case "minecraft:stick":
			sticks++
		case "minecraft:bowl":
			bowls++
		}
	}
	return sticks, bowls
}

// vanilla: RecipeManager.createCheck, CampfireBlock.getTicker
func TestACampfireKeepsItsLastMatchingRecipeForOverlappingInputs(t *testing.T) {
	if sticks, bowls := campfireDrops(t, "minecraft:bone"); sticks != 1 || bowls != 0 {
		t.Errorf("an unprimed campfire dropped %d sticks and %d bowls, want the first recipe's stick", sticks, bowls)
	}
	if sticks, bowls := campfireDrops(t, "minecraft:string", "minecraft:bone"); sticks != 0 || bowls != 2 {
		t.Errorf("a primed campfire dropped %d sticks and %d bowls, want two bowls", sticks, bowls)
	}
}
