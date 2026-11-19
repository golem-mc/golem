package containers

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// openPlaced places the block from the first hotbar slot at (4,64,0) and opens it.
func openPlaced(t *testing.T, srv interface{ TickSurvival() }, w *servertest.WindowedClient, block string, menu int32) {
	t.Helper()
	w.SetCreativeSlot(36, servertest.ItemStack(t, block, 1))
	w.PlaceBlock(4, 63, 0, protocol.FaceTop)
	w.SettleKeeping()
	srv.TickSurvival()
	w.ExpectBlockAck()
	w.UseBlock(4, 64, 0)
	w.ExpectContainerOpen(menu, true, 4, 64, 0)
}

// TestOwnCookingFuelBurnsEightSmelts: a stick carrying cooking_fuel with a
// burn time of 1600 cooks eight raw iron from one stick. vanilla:
// AbstractFurnaceBlockEntity.getBurnDuration
func TestOwnCookingFuelBurnsEightSmelts(t *testing.T) {
	t.Parallel()
	srv, w := servertest.OpenWindowed(t, servertest.BlockTestConfig())
	servertest.StopTheLoop(t, srv)
	openPlaced(t, srv, w, "minecraft:furnace", protocol.MenuFurnace)

	w.SetCreativeSlot(36, servertest.ItemStack(t, "minecraft:raw_iron", 8))
	w.Click(30, 0, protocol.ClickModeQuickMove)
	w.ExpectClickResult(map[int16]protocol.ItemStack{0: servertest.ItemStack(t, "minecraft:raw_iron", 8), 30: {}}, protocol.ItemStack{})
	stick := servertest.ItemStack(t, "minecraft:stick", 1)
	stick.SetCookingFuel(protocol.FuelComponent{Count: 1600, Speed: 1})
	w.SetCreativeSlot(37, stick)
	w.Click(31, 0, protocol.ClickModeQuickMove)
	w.ExpectClickResult(map[int16]protocol.ItemStack{1: stick, 31: {}}, protocol.ItemStack{})
	w.CloseWindow()
	w.ExpectWindowZero()

	runTheSmelts(t, srv, 8*200+2)
	got := srv.InventoryAt(4, 64, 0)
	want := servertest.ItemStack(t, "minecraft:iron_ingot", 8)
	if len(got) != 3 || !got[0].Empty() || !got[1].Empty() || !got[2].Equal(want) {
		t.Errorf("the furnace = %+v, want eight ingots from one stick", got)
	}
}

// TestRemovedCookingFuelIsRefused: coal without cooking_fuel stays on the
// cursor when put in the fuel slot. vanilla: AbstractFurnaceMenu.isFuel
func TestRemovedCookingFuelIsRefused(t *testing.T) {
	t.Parallel()
	srv, w := servertest.OpenWindowedOps(t, servertest.BlockTestConfig())
	servertest.StopTheLoop(t, srv)
	openPlaced(t, srv, w, "minecraft:furnace", protocol.MenuFurnace)

	w.SetCreativeSlot(36, protocol.ItemStack{}) // the furnace item leaves the first slot free
	w.ChatCommand("give @s coal[!cooking_fuel]")
	w.Settle()
	coal := servertest.ItemStack(t, "minecraft:coal", 1)
	coal.Remove(protocol.DataComponentCookingFuel)
	w.Click(30, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{30: {}}, coal)
	w.Click(1, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{}, coal)
}

// TestOwnBrewingFuelLoadsItsUses: a stick with brewing_fuel uses 5 fills the
// stand's fuel to 5 (container property 1). vanilla:
// BrewingStandBlockEntity.serverTick
func TestOwnBrewingFuelLoadsItsUses(t *testing.T) {
	t.Parallel()
	srv, w := servertest.OpenWindowed(t, servertest.BlockTestConfig())
	servertest.StopTheLoop(t, srv)
	openPlaced(t, srv, w, "minecraft:brewing_stand", protocol.MenuBrewingStand)

	stick := servertest.ItemStack(t, "minecraft:stick", 1)
	stick.SetBrewingFuel(protocol.FuelComponent{Count: 5, Speed: 1})
	w.SetCreativeSlot(36, stick)
	w.Click(32, 0, protocol.ClickModeQuickMove)
	w.ExpectClickResult(map[int16]protocol.ItemStack{4: stick, 32: {}}, protocol.ItemStack{})
	srv.TickBlockEntities()
	w.AwaitWindowProperty(1, 5)
}
