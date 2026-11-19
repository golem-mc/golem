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

// TestTakingTheWholeIngredientResetsTheCook takes the whole ingredient out
// with the left click: emptying the slot is vanilla's
// Slot.tryRemove/setByPlayer, a setItem(0, EMPTY) that zeroes the timer and
// recomputes the total to the standard 200 with no input.
func TestTakingTheWholeIngredientResetsTheCook(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		block  string
		menu   int32
		beType int32
		input  string
	}{
		{"furnace", "minecraft:furnace", protocol.MenuFurnace, protocol.BlockEntityFurnace,
			"minecraft:raw_iron"},
		{"blast furnace", "minecraft:blast_furnace", protocol.MenuBlastFurnace, protocol.BlockEntityBlastFurnace,
			"minecraft:raw_iron"},
		{"smoker", "minecraft:smoker", protocol.MenuSmoker, protocol.BlockEntitySmoker,
			"minecraft:beef"},
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
			w.Drain()
			w.Click(0, 0, protocol.ClickModePickup)
			w.ExpectClickResult(map[int16]protocol.ItemStack{
				0: protocol.ItemStack{},
			}, servertest.ItemStack(t, tc.input, 1))

			spent, total, _, _, ok := srv.FurnaceAt(4, 64, 0)
			if !ok {
				t.Fatal("the cooking block's entity went missing")
			}
			if spent != 0 || total != 200 {
				t.Errorf("after taking the ingredient out, cooking = (%d of %d), want (0 of 200)",
					spent, total)
			}
		})
	}
}

// TestTakingHalfTheIngredientOutKeepsTheCook takes the larger half with the
// right click: the leftover is the same item at a smaller count, and the
// write is a removeItem that leaves the timer and total alone.
func TestTakingHalfTheIngredientOutKeepsTheCook(t *testing.T) {
	t.Parallel()
	srv, w := servertest.OpenWindowed(t, servertest.BlockTestConfig())
	servertest.StopTheLoop(t, srv)
	loadCooking(t, srv, w, "minecraft:furnace", protocol.MenuFurnace,
		protocol.BlockEntityFurnace, "minecraft:raw_iron", 2, 1)

	runTheSmelts(t, srv, 50)
	w.UseBlock(4, 64, 0)
	w.ExpectContainerOpenType(protocol.MenuFurnace, protocol.BlockEntityFurnace, 4, 64, 0)
	w.Drain()
	w.Click(0, 1, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{
		0: servertest.ItemStack(t, "minecraft:raw_iron", 1),
	}, servertest.ItemStack(t, "minecraft:raw_iron", 1))

	spent, total, _, _, ok := srv.FurnaceAt(4, 64, 0)
	if !ok {
		t.Fatal("the furnace's entity went missing")
	}
	if spent != 50 || total != 200 {
		t.Errorf("after taking half the ingredient, cooking = (%d of %d), want (50 of 200)", spent, total)
	}
}

// TestASameIngredientAtAnotherCountKeepsTheCook swaps in more of the same
// ingredient with the number key: setItem's isSameItemSameComponents leaves
// the timer and total alone, counts not being part of the comparison.
func TestASameIngredientAtAnotherCountKeepsTheCook(t *testing.T) {
	t.Parallel()
	srv, w := servertest.OpenWindowed(t, servertest.BlockTestConfig())
	servertest.StopTheLoop(t, srv)
	loadCooking(t, srv, w, "minecraft:furnace", protocol.MenuFurnace,
		protocol.BlockEntityFurnace, "minecraft:raw_iron", 1, 1)

	runTheSmelts(t, srv, 50)
	w.UseBlock(4, 64, 0)
	w.ExpectContainerOpenType(protocol.MenuFurnace, protocol.BlockEntityFurnace, 4, 64, 0)
	srv.GiveSlot("Testificate", player.HotbarSlot, servertest.ItemStack(t, "minecraft:raw_iron", 3))
	w.Drain()
	w.Click(0, 0, protocol.ClickModeSwap)
	w.ExpectClickResult(map[int16]protocol.ItemStack{
		0:  servertest.ItemStack(t, "minecraft:raw_iron", 3),
		30: servertest.ItemStack(t, "minecraft:raw_iron", 1),
	}, protocol.ItemStack{})

	spent, total, _, _, ok := srv.FurnaceAt(4, 64, 0)
	if !ok {
		t.Fatal("the furnace's entity went missing")
	}
	if spent != 50 || total != 200 {
		t.Errorf("after swapping in a bigger stack of the same item, cooking = (%d of %d), want (50 of 200)",
			spent, total)
	}
}

// TestAHopperFillingAnEmptyIngredientSlotResetsTheCook has a hopper drop one
// raw iron into an empty ingredient slot: setItem recomputes the total and
// zeroes the timer, a fill being a change from nothing.
func TestAHopperFillingAnEmptyIngredientSlotResetsTheCook(t *testing.T) {
	t.Parallel()
	srv, _ := servertest.OpenWindowed(t, servertest.BlockTestConfig())
	if !srv.PlaceContainer(4, 64, 0, "minecraft:furnace") {
		t.Fatal("could not place the furnace")
	}
	if !srv.PlaceContainer(4, 65, 0, "minecraft:hopper") {
		t.Fatal("could not place the hopper")
	}
	srv.StopTick()
	if !srv.SetInventoryAt(4, 65, 0, 0, servertest.ItemStack(t, "minecraft:raw_iron", 1)) {
		t.Fatal("the hopper has no block entity to fill")
	}
	if !srv.SetCookingAt(4, 64, 0, 50, 200) {
		t.Fatal("the furnace has no block entity to write")
	}

	servertest.TickBlockEntitiesByHand(srv, 1)
	spent, total, _, _, ok := srv.FurnaceAt(4, 64, 0)
	if !ok {
		t.Fatal("the furnace's entity went missing")
	}
	if spent != 0 || total != 200 {
		t.Errorf("after the hopper filled the empty ingredient slot, cooking = (%d of %d), want (0 of 200)",
			spent, total)
	}
}

// TestABlockedOutputZeroesTheCook loads a furnace and blocks its output with
// a dirt: the tick cannot lay the result down, so it zeroes the timer rather
// than cooking on.
func TestABlockedOutputZeroesTheCook(t *testing.T) {
	t.Parallel()
	srv, w := servertest.OpenWindowed(t, servertest.BlockTestConfig())
	servertest.StopTheLoop(t, srv)
	loadCooking(t, srv, w, "minecraft:furnace", protocol.MenuFurnace,
		protocol.BlockEntityFurnace, "minecraft:raw_iron", 1, 1)

	runTheSmelts(t, srv, 50)
	if spent, _, _, _, _ := srv.FurnaceAt(4, 64, 0); spent != 50 {
		t.Fatalf("after 50 ticks, cooking = %d, want 50", spent)
	}
	if !srv.SetInventoryAt(4, 64, 0, 2, servertest.ItemStack(t, "minecraft:dirt", 1)) {
		t.Fatal("the furnace has no result slot to block")
	}

	runTheSmelts(t, srv, 1)
	spent, _, _, _, ok := srv.FurnaceAt(4, 64, 0)
	if !ok {
		t.Fatal("the furnace's entity went missing")
	}
	if spent != 0 {
		t.Errorf("after the blocked tick, cooking = %d, want the reset 0", spent)
	}
}

// TestACookingTimerCoolsOnlyBetweenZeroAndItsTotal runs the decay on a timer
// of one - which the two-a-tick cool takes to zero - and on one above its
// total, which the clamp brings down to the total.
func TestACookingTimerCoolsOnlyBetweenZeroAndItsTotal(t *testing.T) {
	t.Parallel()
	srv, _ := servertest.OpenWindowed(t, servertest.BlockTestConfig())
	if !srv.PlaceContainer(4, 64, 0, "minecraft:furnace") {
		t.Fatal("could not place the furnace")
	}
	srv.StopTick()

	if !srv.SetCookingAt(4, 64, 0, 1, 200) {
		t.Fatal("the furnace has no block entity to write")
	}
	servertest.TickBlockEntitiesByHand(srv, 1)
	if spent, _, _, _, _ := srv.FurnaceAt(4, 64, 0); spent != 0 {
		t.Errorf("a timer of one cooled to %d, want 0", spent)
	}

	if !srv.SetCookingAt(4, 64, 0, 150, 100) {
		t.Fatal("the furnace has no block entity to write")
	}
	servertest.TickBlockEntitiesByHand(srv, 1)
	if spent, total, _, _, _ := srv.FurnaceAt(4, 64, 0); spent != total || spent != 100 {
		t.Errorf("a timer above its total cooled to (%d of %d), want the total 100", spent, total)
	}
}
