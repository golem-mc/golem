package plugins_test

import (
	"sync/atomic"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/plugin"
	"github.com/golem-mc/golem/registry"
)

// vanilla: ServerPlayer.doTick closes a menu that is no longer valid
func TestPluginServerSideCloseFiresInventoryCloseEvent(t *testing.T) {
	srv, h, run, viewer, owner := twoWindowed(t)
	var closes atomic.Int32
	run(func() {
		vp, _ := h.PlayerNamed("Viewer")
		op, _ := h.PlayerNamed("Owner")
		plugin.Listen(h, func(e *plugin.InventoryCloseEvent) { closes.Add(1) })
		vp.OpenInventory(op.Inventory())
	})
	viewer.ExpectContainerOpen(protocol.MenuGeneric9x1+4, false, 0, 0, 0)
	owner.Raw.Close()
	servertest.UntilTicks(t, srv, 40, "the close event", func() bool { return closes.Load() == 1 })
}

// vanilla: AbstractContainerMenu.slotsChanged
func TestPluginStationGridWritable(t *testing.T) {
	_, h, run, w := inventoryPlayer(t)
	run(func() {
		inv := h.CreateInventory(plugin.InvCrafting, 0, plugin.Plain("Bench"), nil)
		v := testerOf(t, h).OpenInventory(inv)
		if v == nil {
			t.Fatal("open refused")
		}
		// The 2x2 at the grid's top left: slots 1, 2, 4, 5 of the 3x3.
		cells := []int{1, 2, 4, 5}
		for _, i := range cells {
			v.Top().SetItem(i, plugin.NewStack(registry.ItemOakPlanks, 1))
		}
		for _, i := range cells {
			if got := v.Top().Item(i); got.Item != registry.ItemOakPlanks {
				t.Errorf("grid slot %d reads %+v", i, got)
			}
		}
		if got := v.Top().Item(0); got.Item != registry.ItemCraftingTable {
			t.Errorf("result reads %+v, want a crafting table", got)
		}
	})
	w.ExpectOpenTitle(protocol.MenuCrafting, 0, 0, 0, 0)
	// Each write recomputes the result; the last one is the table.
	want := servertest.ItemStack(t, "minecraft:crafting_table", 1)
	w.AwaitWindowPackets(func(id int32, data []byte) bool {
		if id != protocol.IDPlayContainerSlot {
			return false
		}
		r := protocol.NewReader(data)
		r.ReadVarInt()
		r.ReadVarInt()
		if slot := r.ReadI16(); slot != 0 {
			return false
		}
		got, ok := r.ReadSlot()
		return ok && got.Equal(want)
	})
}
