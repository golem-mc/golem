package plugins_test

import (
	"sync/atomic"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/plugin"
	"github.com/golem-mc/golem/registry"
)

func inventoryPlayer(t *testing.T) (*server.Server, plugin.Server, func(func()), *servertest.WindowedClient) {
	t.Helper()
	cfg := servertest.BlockTestConfig()
	cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
	srv, h, run := worldPlugin(t, cfg)
	// The loop runs free: a window event is called from the connection
	// goroutine, which then waits for a tick the test cannot count.
	servertest.ReleaseTicks(t, srv)
	w := servertest.WindowedOf(t, "Tester", servertest.DialServer(t, srv))
	return srv, h, run, w
}

func testerOf(t *testing.T, h plugin.Server) plugin.Player {
	t.Helper()
	p, ok := h.PlayerNamed("Tester")
	if !ok {
		t.Fatal("no player")
	}
	return p
}

// vanilla: Inventory, SimpleContainer.addItem, ServerPlayer.setItemSlot.
func TestPluginPlayerInventoryHandles(t *testing.T) {
	_, h, run, w := inventoryPlayer(t)
	run(func() {
		p := testerOf(t, h)
		inv := p.Inventory()
		if inv.Size() != 41 || p.EnderChest().Size() != 27 || inv.Type() != plugin.InvPlayer {
			t.Errorf("sizes %d %d", inv.Size(), p.EnderChest().Size())
		}
		sword := plugin.NewStack(registry.ItemDiamondSword, 1)
		inv.SetItem(3, sword)
		if got := inv.Item(3); got.Item != registry.ItemDiamondSword {
			t.Errorf("slot 3 %+v", got)
		}
		inv.SetHeldSlot(3)
		if inv.HeldSlot() != 3 || p.Equipment().Get(plugin.SlotMainHand).Item != registry.ItemDiamondSword {
			t.Error("held slot or main hand")
		}
		inv.Set(plugin.SlotHead, plugin.NewStack(registry.ItemDiamondHelmet, 1))
		if inv.Get(plugin.SlotHead).Item != registry.ItemDiamondHelmet {
			t.Error("helmet")
		}
		left := inv.Add(plugin.NewStack(registry.ItemStone, 100))
		if len(left) != 0 || inv.Count(registry.ItemStone) != 100 || inv.First(plugin.NewStack(registry.ItemStone, 1)) != 0 {
			t.Errorf("add: left %v count %d first %d", left, inv.Count(registry.ItemStone), inv.First(plugin.NewStack(registry.ItemStone, 1)))
		}
		if miss := inv.Remove(plugin.NewStack(registry.ItemStone, 120)); len(miss) != 1 || miss[0].Count != 20 || inv.Count(registry.ItemStone) != 0 {
			t.Errorf("remove: missing %v", miss)
		}
		p.EnderChest().SetItem(26, plugin.NewStack(registry.ItemStick, 5))
		if p.EnderChest().Item(26).Count != 5 {
			t.Error("ender chest slot")
		}
		p.SetCursor(plugin.NewStack(registry.ItemDirt, 7))
		if p.Cursor().Count != 7 {
			t.Error("cursor")
		}
		inv.Clear()
		if inv.FirstEmpty() != 0 {
			t.Error("clear")
		}
	})
	// The client was told of the slot writes.
	w.Drain()
}

// vanilla: ServerGamePacketListenerImpl.handleContainerClick,
// AbstractContainerMenu.sendAllDataToRemote.
func TestPluginInventoryClickEvents(t *testing.T) {
	srv, h, run, w := inventoryPlayer(t)
	ow := srv.OverworldWorld()
	ow.Acquire(worldChunk0)
	defer ow.Release(worldChunk0)
	placeContainerEntity(ow, 4, 64, 0, registry.BlockChest)
	var opened, closed, seen atomic.Int32
	var clicks []*plugin.InventoryClickEvent
	cancel := true
	var chest plugin.Inventory
	run(func() {
		c := containerHandle(t, h.Overworld(), plugin.BlockPos{X: 4, Y: 64})
		chest = c.Inventory()
		chest.SetItem(0, plugin.NewStack(registry.ItemStick, 3))
		plugin.Listen(h, func(e *plugin.InventoryOpenEvent) { opened.Add(1) })
		plugin.Listen(h, func(e *plugin.InventoryCloseEvent) { closed.Add(1) })
		plugin.Listen(h, func(e *plugin.InventoryClickEvent) {
			clicks = append(clicks, e)
			seen.Add(1)
			e.SetCancelled(cancel)
		})
	})
	w.UseBlock(4, 64, 0)
	servertest.UntilTicks(t, srv, 20, "the open event", func() bool { return opened.Load() == 1 })
	w.ExpectContainerOpenType(protocol.MenuGeneric9x3, protocol.BlockEntityChest, 4, 64, 0)

	w.Click(0, 0, protocol.ClickModePickup)
	servertest.UntilTicks(t, srv, 20, "the click event", func() bool { return seen.Load() == 1 })
	if slots := w.ExpectWindowContent(); slots[0].Count != 3 {
		t.Errorf("the resync shows %+v in slot 0", slots[0])
	}
	if len(clicks) != 1 || clicks[0].RawSlot != 0 || clicks[0].Slot != 0 || clicks[0].Click != plugin.ClickLeft ||
		clicks[0].Current.Item != registry.ItemStick || clicks[0].Inventory.Type() != plugin.InvChest {
		t.Fatalf("click event %+v", clicks)
	}
	run(func() {
		if chest.Item(0).Count != 3 || testerOf(t, h).Cursor().Count != 0 {
			t.Error("a cancelled click changed something")
		}
	})

	cancel = false
	w.Click(0, 0, protocol.ClickModePickup)
	servertest.UntilTicks(t, srv, 20, "the second click event", func() bool { return seen.Load() == 2 })
	w.ExpectClickResult(map[int16]protocol.ItemStack{0: {}}, servertest.ItemStack(t, "minecraft:stick", 3))
	run(func() {
		if !chest.Item(0).Empty() || testerOf(t, h).Cursor().Count != 3 {
			t.Errorf("allowed click: slot %+v cursor %+v", chest.Item(0), testerOf(t, h).Cursor())
		}
	})
	w.Drain()
	w.CloseWindow()
	servertest.UntilTicks(t, srv, 20, "the close event", func() bool { return closed.Load() == 1 })
}

var worldChunk0 = world.ChunkPos{}

// vanilla: AbstractContainerMenu.doClick QUICK_CRAFT end step.
func TestPluginInventoryDragEvent(t *testing.T) {
	srv, h, run, w := inventoryPlayer(t)
	ow := srv.OverworldWorld()
	ow.Acquire(worldChunk0)
	defer ow.Release(worldChunk0)
	placeContainerEntity(ow, 4, 64, 0, registry.BlockChest)
	var opened, seen atomic.Int32
	var drags []*plugin.InventoryDragEvent
	cancel, edit := true, false
	var chest plugin.Inventory
	run(func() {
		chest = containerHandle(t, h.Overworld(), plugin.BlockPos{X: 4, Y: 64}).Inventory()
		plugin.Listen(h, func(e *plugin.InventoryOpenEvent) { opened.Add(1) })
		plugin.Listen(h, func(e *plugin.InventoryDragEvent) {
			drags = append(drags, e)
			seen.Add(1)
			e.SetCancelled(cancel)
			if edit {
				e.Slots[0] = plugin.NewStack(registry.ItemStick, 1)
				e.Cursor = plugin.NewStack(registry.ItemStick, 1)
			}
		})
	})
	w.UseBlock(4, 64, 0)
	servertest.UntilTicks(t, srv, 20, "the open event", func() bool { return opened.Load() == 1 })
	w.ExpectContainerOpenType(protocol.MenuGeneric9x3, protocol.BlockEntityChest, 4, 64, 0)
	run(func() { testerOf(t, h).SetCursor(plugin.NewStack(registry.ItemStick, 4)) })
	w.Drain()

	drag := func() {
		w.Click(-999, 0, protocol.ClickModeDrag)
		w.Click(0, 1, protocol.ClickModeDrag)
		w.Click(1, 1, protocol.ClickModeDrag)
		w.Click(-999, 2, protocol.ClickModeDrag)
	}
	drag()
	servertest.UntilTicks(t, srv, 20, "the drag event", func() bool { return seen.Load() == 1 })
	if len(drags) != 1 || len(drags[0].Slots) != 2 || drags[0].Slots[0].Count != 2 || drags[0].Slots[1].Item != registry.ItemStick ||
		drags[0].Cursor.Count != 0 || drags[0].Right {
		t.Fatalf("drag event %+v", drags)
	}
	w.ExpectWindowContent()
	run(func() {
		if !chest.Item(0).Empty() || !chest.Item(1).Empty() || testerOf(t, h).Cursor().Count != 4 {
			t.Error("a cancelled drag changed something")
		}
	})

	cancel = false
	drag()
	servertest.UntilTicks(t, srv, 20, "the second drag event", func() bool { return seen.Load() == 2 })
	w.AwaitWindowSlot(0, servertest.ItemStack(t, "minecraft:stick", 2))
	run(func() {
		if chest.Item(0).Count != 2 || chest.Item(1).Count != 2 || testerOf(t, h).Cursor().Count != 0 {
			t.Errorf("allowed drag: %+v %+v cursor %+v", chest.Item(0), chest.Item(1), testerOf(t, h).Cursor())
		}
	})

	// A listener's edit to the marked slots and the cursor is what is written.
	edit = true
	run(func() { testerOf(t, h).SetCursor(plugin.NewStack(registry.ItemStick, 4)) })
	w.Drain()
	chestSlot := func(i int) plugin.ItemStack { return chest.Item(i) }
	run(func() { chest.Clear() })
	w.Drain()
	drag()
	servertest.UntilTicks(t, srv, 20, "the third drag event", func() bool { return seen.Load() == 3 })
	run(func() {
		if chestSlot(0).Count != 1 || chestSlot(1).Count != 2 || testerOf(t, h).Cursor().Count != 1 {
			t.Errorf("edited drag: %+v %+v cursor %+v", chestSlot(0), chestSlot(1), testerOf(t, h).Cursor())
		}
	})
}
