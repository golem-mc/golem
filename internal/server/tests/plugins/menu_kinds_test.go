package plugins_test

import (
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/plugin"
	"github.com/golem-mc/golem/registry"
)

func twoWindowed(t *testing.T) (*server.Server, plugin.Server, func(func()), *servertest.WindowedClient, *servertest.WindowedClient) {
	t.Helper()
	cfg := servertest.BlockTestConfig()
	cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
	srv, h, run := worldPlugin(t, cfg)
	servertest.ReleaseTicks(t, srv)
	a := servertest.WindowedOf(t, "Viewer", servertest.DialServer(t, srv))
	b := servertest.WindowedOf(t, "Owner", servertest.DialServer(t, srv))
	return srv, h, run, a, b
}

// vanilla: AnvilMenu, ContainerLevelAccess.NULL
func TestPluginStationMenus(t *testing.T) {
	cases := []struct {
		kind plugin.InventoryType
		menu int32
	}{
		{plugin.InvCrafting, protocol.MenuCrafting}, {plugin.InvAnvil, protocol.MenuAnvil},
		{plugin.InvGrindstone, protocol.MenuGrindstone}, {plugin.InvLoom, protocol.MenuLoom},
		{plugin.InvSmithing, protocol.MenuSmithing}, {plugin.InvStonecutter, protocol.MenuStonecutter},
		{plugin.InvCartography, protocol.MenuCartography},
		{plugin.InvEnchanting, protocol.MenuEnchantment}, {plugin.InvBeacon, protocol.MenuBeacon},
		{plugin.InvMerchant, protocol.MenuMerchant},
	}
	_, h, run, w := inventoryPlayer(t)
	for _, tc := range cases {
		run(func() {
			inv := h.CreateInventory(tc.kind, 0, plugin.Plain("Virtual"), nil)
			if inv == nil {
				t.Errorf("kind %d: no inventory", tc.kind)
				return
			}
			v := testerOf(t, h).OpenInventory(inv)
			if v == nil || v.Top().Type() != tc.kind {
				t.Errorf("kind %d: view %v", tc.kind, v)
			}
		})
		_, title := w.ExpectOpenTitle(tc.menu, 0, 0, 0, 0)
		if title.Text != "Virtual" {
			t.Errorf("kind %d: title %+v", tc.kind, title)
		}
		run(func() {
			if _, ok := testerOf(t, h).OpenView(); !ok {
				t.Errorf("kind %d: window closed with no block", tc.kind)
			}
		})
		w.CloseWindow()
	}
}

// vanilla: ServerPlayer.openMenu with a ChestMenu over another player's inventory
func TestPluginInvseeAndEnderChest(t *testing.T) {
	srv, h, run, viewer, _ := twoWindowed(t)
	var vp, op plugin.Player
	run(func() {
		vp, _ = h.PlayerNamed("Viewer")
		op, _ = h.PlayerNamed("Owner")
		op.Inventory().SetItem(0, plugin.NewStack(registry.ItemStick, 3))
		op.EnderChest().SetItem(2, plugin.NewStack(registry.ItemApple, 5))
		if v := vp.OpenInventory(op.Inventory()); v == nil {
			t.Error("invsee refused")
		}
	})
	slots := viewer.ExpectContainerOpen(protocol.MenuGeneric9x1+4, false, 0, 0, 0)
	run(func() {
		if v, _ := vp.OpenView(); v.Top().Type() != plugin.InvPlayer || v.Top().Holder() == nil {
			t.Errorf("invsee top type %v, want InvPlayer", v.Top().Type())
		}
	})
	if slots[0].Count != 3 {
		t.Fatalf("invsee slot 0 %+v", slots[0])
	}
	// The owner's later change reaches the viewer, and the viewer's reaches the owner.
	run(func() { op.Inventory().SetItem(1, plugin.NewStack(registry.ItemStick, 1)) })
	viewer.AwaitWindowSlot(1, servertest.ItemStack(t, "minecraft:stick", 1))
	run(func() {
		v, _ := vp.OpenView()
		v.Top().SetItem(2, plugin.NewStack(registry.ItemApple, 2))
	})
	servertest.UntilTicks(t, srv, 20, "the write to reach the owner", func() bool {
		return op.Inventory().Item(2).Count == 2
	})
}

// vanilla: HopperMenu, DispenserMenu, ShulkerBoxMenu over a SimpleContainer
func TestPluginContainerKindMenus(t *testing.T) {
	cases := []struct {
		kind  plugin.InventoryType
		menu  int32
		slots int
	}{
		{plugin.InvHopper, protocol.MenuHopper, 5}, {plugin.InvDispenser, protocol.MenuGeneric3x3, 9},
		{plugin.InvDropper, protocol.MenuGeneric3x3, 9}, {plugin.InvShulkerBox, protocol.MenuShulkerBox, 27},
		{plugin.InvFurnace, protocol.MenuFurnace, 3}, {plugin.InvBlastFurnace, protocol.MenuBlastFurnace, 3},
		{plugin.InvSmoker, protocol.MenuSmoker, 3}, {plugin.InvBrewing, protocol.MenuBrewingStand, 5},
		{plugin.InvCrafter, protocol.MenuCrafter, 9}, {plugin.InvLectern, protocol.MenuLectern, 1},
	}
	_, h, run, w := inventoryPlayer(t)
	for _, tc := range cases {
		run(func() {
			inv := h.CreateInventory(tc.kind, 0, plugin.Plain("Own"), nil)
			if inv == nil || inv.Size() != tc.slots || inv.Type() != tc.kind {
				t.Errorf("kind %d: inventory %v", tc.kind, inv)
				return
			}
			inv.SetItem(0, plugin.ItemStack{Item: registry.ItemStick, Count: 3})
			v := testerOf(t, h).OpenInventory(inv)
			if v == nil || v.Top().Type() != tc.kind || v.Top().Item(0).Count != 3 {
				t.Errorf("kind %d: type %d item %+v", tc.kind, v.Top().Type(), v.Top().Item(0))
			}
		})
		if _, title := w.ExpectOpenTitle(tc.menu, 0, 0, 0, 0); title.Text != "Own" {
			t.Errorf("kind %d: title %+v", tc.kind, title)
		}
		w.CloseWindow()
	}
}
