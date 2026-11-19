package plugins_test

import (
	"sync/atomic"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/plugin"
	"github.com/golem-mc/golem/registry"
)

// vanilla: ChestMenu.threeRows, ServerGamePacketListenerImpl.handleContainerClick.
func TestPluginCustomMenu(t *testing.T) {
	t.Parallel()
	srv, h, run, w := inventoryPlayer(t)
	var menu plugin.Inventory
	var clicks atomic.Int32
	var closed atomic.Bool
	cancel := true
	run(func() {
		menu = h.CreateInventory(plugin.InvChest, 2, plugin.Text{}, "shop")
		menu.SetItem(0, plugin.NewStack(registry.ItemStick, 3))
		plugin.Listen(h, func(e *plugin.InventoryClickEvent) {
			if e.View.Top() == menu {
				clicks.Add(1)
				e.SetCancelled(cancel)
			}
		})
		plugin.Listen(h, func(e *plugin.InventoryCloseEvent) { closed.Store(true) })
		if v := testerOf(t, h).OpenInventory(menu); v == nil || v.Top().Holder() != "shop" {
			t.Errorf("open answered %v", v)
		}
	})
	slots := w.ExpectContainerOpen(protocol.MenuGeneric9x1+1, false, 0, 0, 0)
	if len(slots) != 18+36 || slots[0].Count != 3 {
		t.Fatalf("menu content: %d slots, first %+v", len(slots), slots[0])
	}

	// A cancelled click changes nothing and resyncs the window.
	w.Click(0, 0, protocol.ClickModePickup)
	servertest.UntilTicks(t, srv, 20, "the click event", func() bool { return clicks.Load() == 1 })
	w.ExpectWindowContent()
	if menu.Item(0).Count != 3 {
		t.Errorf("a cancelled click moved the stack: %+v", menu.Item(0))
	}

	// A plugin write reaches the open window.
	run(func() { menu.SetItem(1, plugin.NewStack(registry.ItemStick, 1)) })
	w.AwaitWindowSlot(1, servertest.ItemStack(t, "minecraft:stick", 1))

	cancel = false
	w.Click(0, 0, protocol.ClickModePickup)
	w.AwaitCursorItem(servertest.ItemStack(t, "minecraft:stick", 3))
	if !menu.Item(0).Empty() {
		t.Errorf("an allowed click left %+v", menu.Item(0))
	}
	run(func() { testerOf(t, h).CloseInventory() })
	if !closed.Load() {
		t.Error("no close event")
	}
}
