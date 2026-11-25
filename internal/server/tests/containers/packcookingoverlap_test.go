package containers

import (
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// cookTicks is the first overlap recipe's cooking time; the second takes one
// tick less, so a duration taken from the wrong recipe shows.
const cookTicks, laterCookTicks = 10, 9

// overlapPack writes two recipes of kind taking bone, the first making a stick
// and the second, whose other input is string, a bowl.
func overlapPack(t *testing.T, cfg *config.Config, kind string) {
	t.Helper()
	servertest.WritePackFile(t, cfg, "p", "pk/recipe/a.json", `{"type":"minecraft:`+kind+`","ingredient":["minecraft:feather","minecraft:bone"],"cookingtime":10,"result":{"id":"minecraft:stick"}}`)
	servertest.WritePackFile(t, cfg, "p", "pk/recipe/b.json", `{"type":"minecraft:`+kind+`","ingredient":["minecraft:bone","minecraft:string"],"cookingtime":9,"result":{"id":"minecraft:bowl"}}`)
}

// vanilla: RecipeManager.createCheck, AbstractFurnaceBlockEntity.setItem, serverTick
func TestAFurnaceKeepsItsLastMatchingRecipeForOverlappingInputs(t *testing.T) {
	t.Cleanup(func() { registry.ApplyPackRecipes(registry.PackRecipes{}) })
	cfg := servertest.BlockTestConfig()
	cfg.LevelName = t.TempDir()
	overlapPack(t, cfg, "smelting")
	srv, c := servertest.Dial(t, cfg, servertest.OpsListing(t, "Testificate"))
	w := servertest.WindowedOf(t, "Testificate", c)
	stick := servertest.ItemStack(t, "minecraft:stick", 1)
	bowl := servertest.ItemStack(t, "minecraft:bowl", 1)
	const fresh, primed, idle = 2, 3, 4
	for _, x := range []int32{fresh, primed} {
		if !srv.PlaceContainer(x, 64, 0, "minecraft:furnace") {
			t.Fatal("could not place the furnace")
		}
	}
	// The input is loaded from data, which looks no recipe up.
	servertest.Setblock(t, c, idle, 64, 0, `furnace{Items:[{Slot:0b,id:"minecraft:string",count:1}]}`)
	// Window slots 30..34 are the hotbar slots 36..40.
	for i, item := range []string{"minecraft:bone", "minecraft:bone", "minecraft:string", "minecraft:coal", "minecraft:coal", "minecraft:coal", "minecraft:bone"} {
		w.SetCreativeSlot(int16(36+i), servertest.ItemStack(t, item, 1))
	}
	w.Settle()
	servertest.StopTheLoop(t, srv)
	// load shift-clicks hotbar slot i into the open furnace's input or fuel.
	load := func(x int32, hotbar ...int16) {
		w.Settle()
		w.UseBlock(x, 64, 0)
		w.ExpectContainerOpen(protocol.MenuFurnace, true, x, 64, 0)
		for _, slot := range hotbar {
			w.Click(30+slot, 0, protocol.ClickModeQuickMove)
			w.CollectClickResponse()
		}
		w.CloseWindow()
	}
	output := func(x int32) protocol.ItemStack {
		t.Helper()
		got := srv.InventoryAt(x, 64, 0)
		if len(got) != 3 {
			t.Fatalf("the furnace at %d holds %+v", x, got)
		}
		return got[2]
	}

	load(fresh, 0, 3)
	load(primed, 2, 4)
	servertest.TickBlockEntitiesByHand(srv, cookTicks)
	// The idle furnace held string without fuel through the round; its string
	// is taken out by hand and bone put in, as the primed furnace gets bone.
	w.Settle()
	w.UseBlock(idle, 64, 0)
	w.ExpectContainerOpen(protocol.MenuFurnace, true, idle, 64, 0)
	w.Click(0, 0, protocol.ClickModeQuickMove)
	w.CollectClickResponse()
	w.CloseWindow()
	load(idle, 6, 5)
	load(primed, 1)
	servertest.TickBlockEntitiesByHand(srv, laterCookTicks)
	bowls := bowl
	bowls.Count = 2
	if got := output(primed); !got.Equal(bowls) {
		t.Errorf("a primed furnace's output is %+v after %d ticks, want two bowls from the later recipe's cook time", got, laterCookTicks)
	}
	servertest.TickBlockEntitiesByHand(srv, cookTicks-laterCookTicks)
	if got := output(fresh); !got.Equal(stick) {
		t.Errorf("an unprimed furnace made %+v, want the first recipe's stick", got)
	}
	if got := output(primed); !got.Equal(bowls) {
		t.Errorf("a primed furnace's output is %+v after bone, want two bowls", got)
	}
	if got := output(idle); !got.Equal(stick) {
		t.Errorf("an idle furnace that held string made %+v from bone, want the first recipe's stick", got)
	}
}

// vanilla: RecipeManager.createCheck, CampfireBlock.getTicker, LevelChunk.updateBlockEntityTicker
func TestACampfireKeepsItsLastMatchingRecipeForOverlappingInputs(t *testing.T) {
	t.Cleanup(func() { registry.ApplyPackRecipes(registry.PackRecipes{}) })
	cfg := servertest.SurvivalTestConfig()
	cfg.LevelName = t.TempDir()
	overlapPack(t, cfg, "campfire_cooking")
	srv, w := servertest.OpenWindowed(t, cfg)
	srv.StopTick()
	lit, ok := registry.FindState("minecraft:campfire", map[string]string{"lit": "true"})
	if !ok {
		t.Fatal("no lit campfire state")
	}
	out, _ := registry.FindState("minecraft:campfire", map[string]string{"lit": "false"})
	const fresh, primed, relit = 2, 3, 4
	for _, x := range []int32{fresh, primed, relit} {
		srv.SetBlock(x, 64, 0, lit)
	}
	// put loads one food onto the campfire at x.
	put := func(x int32, food string) {
		srv.GiveSlot("Testificate", player.HotbarSlot, servertest.ItemStack(t, food, 1))
		w.Settle()
		w.UseBlock(x, 64, 0)
		w.Settle()
	}
	put(fresh, "minecraft:bone")
	put(primed, "minecraft:string")
	put(relit, "minecraft:string")
	servertest.TickBlockEntitiesByHand(srv, cookTicks)
	srv.SetBlock(relit, 64, 0, out)
	srv.SetBlock(relit, 64, 0, lit)
	put(primed, "minecraft:bone")
	put(relit, "minecraft:bone")
	servertest.TickBlockEntitiesByHand(srv, cookTicks)

	drops := func(x int32) (sticks, bowls int) {
		for _, it := range srv.ItemsInDimension("minecraft:overworld") {
			if it.X < float64(x) || it.X >= float64(x+1) {
				continue
			}
			switch it.Item {
			case "minecraft:stick":
				sticks++
			case "minecraft:bowl":
				bowls++
			}
		}
		return sticks, bowls
	}
	if sticks, bowls := drops(fresh); sticks != 1 || bowls != 0 {
		t.Errorf("an unprimed campfire dropped %d sticks and %d bowls, want the first recipe's stick", sticks, bowls)
	}
	if sticks, bowls := drops(primed); sticks != 0 || bowls != 2 {
		t.Errorf("a primed campfire dropped %d sticks and %d bowls, want two bowls", sticks, bowls)
	}
	if sticks, bowls := drops(relit); sticks != 1 || bowls != 1 {
		t.Errorf("a relit campfire dropped %d sticks and %d bowls, want a bowl then a stick", sticks, bowls)
	}
}
