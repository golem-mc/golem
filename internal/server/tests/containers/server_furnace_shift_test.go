package containers

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
)

// TestShiftClickOutOfAFurnaceSendsInputAndFuelForwards checks which end of
// the player's things a shift-click out of a furnace window fills: the
// ingredient and the fuel walk forwards from window slot 3, the first
// main-inventory slot, while the output still walks backwards into the
// hotbar's far end. vanilla: AbstractFurnaceMenu.quickMoveStack
func TestShiftClickOutOfAFurnaceSendsInputAndFuelForwards(t *testing.T) {
	t.Parallel()
	srv, w := servertest.OpenWindowed(t, servertest.BlockTestConfig())
	servertest.StopTheLoop(t, srv)

	w.SetCreativeSlot(36, servertest.ItemStack(t, "minecraft:furnace", 1))
	w.PlaceBlock(4, 63, 0, protocol.FaceTop)
	w.SettleKeeping()
	srv.TickSurvival()
	w.ExpectBlockAck()

	iron := servertest.ItemStack(t, "minecraft:raw_iron", 1)
	coal := servertest.ItemStack(t, "minecraft:coal", 5)
	if !srv.SetInventoryAt(4, 64, 0, 0, iron) {
		t.Fatal("the furnace has no ingredient slot to fill")
	}
	if !srv.SetInventoryAt(4, 64, 0, 1, coal) {
		t.Fatal("the furnace has no fuel slot to fill")
	}

	w.UseBlock(4, 64, 0)
	w.ExpectContainerOpen(protocol.MenuFurnace, true, 4, 64, 0)

	// The ingredient walks forwards to the first main-inventory slot, 3.
	w.Click(0, 0, protocol.ClickModeQuickMove)
	w.ExpectClickResult(map[int16]protocol.ItemStack{
		0: protocol.ItemStack{},
		3: iron,
	}, protocol.ItemStack{})

	// The fuel walks forwards too, to the next free one, 4.
	w.Click(1, 0, protocol.ClickModeQuickMove)
	w.ExpectClickResult(map[int16]protocol.ItemStack{
		1: protocol.ItemStack{},
		4: coal,
	}, protocol.ItemStack{})
}
