package plugins_test

import (
	"sync/atomic"
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/plugin"
	"github.com/golem-mc/golem/registry"
)

// vanilla: HopperBlockEntity.suckInItems, HopperBlockEntity.ejectItems.
func TestPluginHopperMoveEvents(t *testing.T) {
	t.Parallel()
	srv, h, run, _ := inventoryPlayer(t)
	ow := srv.OverworldWorld()
	ow.Acquire(worldChunk0)
	defer ow.Release(worldChunk0)
	placeContainerEntity(ow, 4, 66, 0, registry.BlockChest)
	placeContainerEntity(ow, 4, 65, 0, registry.BlockHopper)
	placeContainerEntity(ow, 4, 64, 0, registry.BlockChest)
	var above, hopper, below plugin.Inventory
	var moves []*plugin.InventoryMoveItemEvent
	var cancel atomic.Bool
	cancel.Store(true)
	run(func() {
		w := h.Overworld()
		above = containerHandle(t, w, plugin.BlockPos{X: 4, Y: 66}).Inventory()
		hopper = containerHandle(t, w, plugin.BlockPos{X: 4, Y: 65}).Inventory()
		below = containerHandle(t, w, plugin.BlockPos{X: 4, Y: 64}).Inventory()
		above.SetItem(0, plugin.NewStack(registry.ItemStick, 2))
		plugin.Listen(h, func(e *plugin.InventoryMoveItemEvent) {
			moves = append(moves, e)
			e.SetCancelled(cancel.Load())
		})
	})
	servertest.RunTicks(t, srv, 40)
	run(func() {
		if len(moves) == 0 || !above.Item(0).Empty() && above.Item(0).Count != 2 || !hopper.Item(0).Empty() {
			t.Errorf("a cancelled pull moved something: above %+v hopper %+v", above.Item(0), hopper.Item(0))
		}
		if len(moves) == 0 || moves[0].Item.Item != registry.ItemStick || moves[0].Item.Count != 1 {
			t.Errorf("events %+v", moves)
		}
	})
	cancel.Store(false)
	servertest.RunTicks(t, srv, 40)
	run(func() {
		if got := below.Item(0); got.Item != registry.ItemStick || got.Count != 2 || !above.Item(0).Empty() || !hopper.Item(0).Empty() {
			t.Errorf("allowed moves: below %+v above %+v hopper %+v", got, above.Item(0), hopper.Item(0))
		}
		var push, pull bool
		for _, e := range moves {
			if e.Initiator == nil || e.Initiator.Holder() != hopper.Holder() {
				t.Errorf("initiator wrong: got %v, want hopper %v", e.Initiator, hopper.Holder())
			}
			if e.Source.Holder() == below.Holder() || e.Destination.Holder() == above.Holder() {
				t.Errorf("event direction wrong: %+v", e)
			}
			push = push || e.Destination.Holder() == below.Holder()
			pull = pull || e.Source.Holder() == above.Holder()
		}
		if !push || !pull {
			t.Errorf("push %v pull %v", push, pull)
		}
	})
}
