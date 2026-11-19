package containers

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
)

// A cooking block's progress when it stops making headway: an unlit furnace
// with nothing it can pair cools what it had by two a tick, and a changed
// ingredient starts the cook over. Both are vanilla's
// AbstractFurnaceBlockEntity.serverTick and setItem.

// TestACookingBlocksProgressDecaysWithoutFuel runs a cooking block on a fuel
// shorter than its recipe until the fuel is spent, then ten ticks more: the
// progress it reached cools by two a tick rather than standing still. The
// blast furnace and the smoker burn a fuel in half its time, bamboo's fifty
// ticks in twenty-five.
func TestACookingBlocksProgressDecaysWithoutFuel(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name      string
		block     string
		menu      int32
		beType    int32
		input     string
		fuel      string
		burnTicks int32
		cookTicks int32
	}{
		{"furnace", "minecraft:furnace", protocol.MenuFurnace, protocol.BlockEntityFurnace,
			"minecraft:raw_iron", "minecraft:stick", 100, 200},
		{"blast furnace", "minecraft:blast_furnace", protocol.MenuBlastFurnace, protocol.BlockEntityBlastFurnace,
			"minecraft:raw_iron", "minecraft:bamboo", 25, 100},
		{"smoker", "minecraft:smoker", protocol.MenuSmoker, protocol.BlockEntitySmoker,
			"minecraft:beef", "minecraft:bamboo", 25, 100},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv, w := servertest.OpenWindowed(t, servertest.BlockTestConfig())
			servertest.StopTheLoop(t, srv)
			loadCooking(t, srv, w, tc.block, tc.menu, tc.beType, tc.input, 1, 1)
			srv.SetInventoryAt(4, 64, 0, 1, servertest.ItemStack(t, tc.fuel, 1))

			// The tick the fuel runs out is the last one that counts: the
			// progress stands at the fuel's burn and a single tick is left.
			runTheSmelts(t, srv, int(tc.burnTicks))
			spent, total, lit, _, ok := srv.FurnaceAt(4, 64, 0)
			if !ok {
				t.Fatal("the cooking block's entity went missing")
			}
			if spent != tc.burnTicks || total != tc.cookTicks {
				t.Fatalf("when the fuel runs out, cooking = (%d of %d), want (%d of %d)",
					spent, total, tc.burnTicks, tc.cookTicks)
			}
			if lit != 1 {
				t.Fatalf("when the fuel runs out, %d fuel ticks remain, want the last", lit)
			}

			runTheSmelts(t, srv, 10)
			if spent, _, _, _, _ := srv.FurnaceAt(4, 64, 0); spent != tc.burnTicks-20 {
				t.Errorf("ten ticks with nothing to smelt, cooking = %d, want %d",
					spent, tc.burnTicks-20)
			}
		})
	}
}

// TestACookingBlocksProgressResetsOnANewIngredient cooks half a recipe, then
// swaps in a different ingredient with the number-key click: the new item
// zeroes the progress and names its own cook total, and the tick that follows
// is the first of the new smelt.
func TestACookingBlocksProgressResetsOnANewIngredient(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		block   string
		menu    int32
		beType  int32
		input   string
		replace string
		cook    int32
	}{
		{"furnace", "minecraft:furnace", protocol.MenuFurnace, protocol.BlockEntityFurnace,
			"minecraft:raw_iron", "minecraft:raw_copper", 200},
		{"blast furnace", "minecraft:blast_furnace", protocol.MenuBlastFurnace, protocol.BlockEntityBlastFurnace,
			"minecraft:raw_iron", "minecraft:raw_copper", 100},
		{"smoker", "minecraft:smoker", protocol.MenuSmoker, protocol.BlockEntitySmoker,
			"minecraft:beef", "minecraft:porkchop", 100},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv, w := servertest.OpenWindowed(t, servertest.BlockTestConfig())
			servertest.StopTheLoop(t, srv)
			loadCooking(t, srv, w, tc.block, tc.menu, tc.beType, tc.input, 1, 1)

			runTheSmelts(t, srv, 50)
			if spent, _, _, _, _ := srv.FurnaceAt(4, 64, 0); spent != 50 {
				t.Fatalf("after 50 ticks, cooking = %d, want 50", spent)
			}

			w.UseBlock(4, 64, 0)
			w.ExpectContainerOpenType(tc.menu, tc.beType, 4, 64, 0)
			srv.GiveSlot("Testificate", player.HotbarSlot, servertest.ItemStack(t, tc.replace, 1))
			w.Drain()
			w.Click(0, 0, protocol.ClickModeSwap)
			w.ExpectClickResult(map[int16]protocol.ItemStack{
				0:  servertest.ItemStack(t, tc.replace, 1),
				30: servertest.ItemStack(t, tc.input, 1),
			}, protocol.ItemStack{})

			spent, total, _, _, ok := srv.FurnaceAt(4, 64, 0)
			if !ok {
				t.Fatal("the cooking block's entity went missing")
			}
			if spent != 0 {
				t.Errorf("after the ingredient changed, cooking = %d, want the reset 0", spent)
			}
			if total != tc.cook {
				t.Errorf("after the ingredient changed, the cook total = %d, want %d", total, tc.cook)
			}

			runTheSmelts(t, srv, 1)
			if spent, _, _, _, _ := srv.FurnaceAt(4, 64, 0); spent != 1 {
				t.Errorf("the tick after the change left cooking = %d, want 1", spent)
			}
		})
	}
}
