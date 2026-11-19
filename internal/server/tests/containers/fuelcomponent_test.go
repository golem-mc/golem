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
