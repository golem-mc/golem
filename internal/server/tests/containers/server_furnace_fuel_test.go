package containers

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
)

// TestFurnaceBurnsAWoodenPickaxe shift-clicks a wooden pickaxe into a
// furnace beside a raw iron: the pickaxe goes to the fuel slot, and its 200
// ticks of burn see the 200-tick smelt through. vanilla: FuelValues
func TestFurnaceBurnsAWoodenPickaxe(t *testing.T) {
	t.Parallel()
	srv, w := servertest.OpenWindowed(t, servertest.BlockTestConfig())
	servertest.StopTheLoop(t, srv)

	w.SetCreativeSlot(36, servertest.ItemStack(t, "minecraft:furnace", 1))
	w.PlaceBlock(4, 63, 0, protocol.FaceTop)
	// The loop is stopped, so the placement's acknowledgement leaves with
	// the tick's survival pass the test runs itself; the settle is what
	// waits for the placement to have been handled first.
	w.SettleKeeping()
	srv.TickSurvival()
	w.ExpectBlockAck()
	w.UseBlock(4, 64, 0)
	w.ExpectContainerOpen(protocol.MenuFurnace, true, 4, 64, 0)

	w.SetCreativeSlot(36, servertest.ItemStack(t, "minecraft:raw_iron", 1))
	w.Click(30, 0, protocol.ClickModeQuickMove)
	w.ExpectClickResult(map[int16]protocol.ItemStack{
		0:  servertest.ItemStack(t, "minecraft:raw_iron", 1),
		30: protocol.ItemStack{},
	}, protocol.ItemStack{})
	w.SetCreativeSlot(37, servertest.ItemStack(t, "minecraft:wooden_pickaxe", 1))
	w.Click(31, 0, protocol.ClickModeQuickMove)
	w.ExpectClickResult(map[int16]protocol.ItemStack{
		1:  servertest.ItemStack(t, "minecraft:wooden_pickaxe", 1),
		31: protocol.ItemStack{},
	}, protocol.ItemStack{})
	w.CloseWindow()
	w.ExpectWindowZero()

	runTheSmelts(t, srv, 200)

	got := srv.InventoryAt(4, 64, 0)
	want := []protocol.ItemStack{{}, {}, servertest.ItemStack(t, "minecraft:iron_ingot", 1)}
	if len(got) != 3 || !got[0].Equal(want[0]) || !got[1].Equal(want[1]) || !got[2].Equal(want[2]) {
		t.Errorf("the furnace after 200 ticks = %+v, want the pickaxe burnt and one ingot out", got)
	}
}
