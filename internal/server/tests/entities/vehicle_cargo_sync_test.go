package entities

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// vanilla: AbstractContainerMenu.broadcastChanges, ContainerEntity
func TestTwoPlayersSeeEachOthersVehicleCargoChanges(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"minecraft:chest_minecart", "minecraft:oak_chest_boat", "minecraft:hopper_minecart"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			srv, connect := servertest.Start(t, servertest.BlockTestConfig())
			alice := servertest.WindowedOf(t, "Alice", connect())
			bob := servertest.WindowedOf(t, "Bob", connect())
			servertest.HoldTicks(t, srv)
			var id int32
			menu, hotbar, slot := int32(protocol.MenuGeneric9x3), int16(54), int16(7)
			if kind == "minecraft:oak_chest_boat" {
				id = srv.SpawnBoat(4.5, 64, 1.5, kind)
			} else {
				servertest.CartRails(t, srv, 3, 6)
				srv.RunLine("summon " + kind + " 4.5 64.0625 1.5")
				for _, cart := range srv.MinecartsInDimension(world.DimensionName) {
					id = cart.ID
				}
				if kind == "minecraft:hopper_minecart" {
					menu, hotbar, slot = protocol.MenuHopper, 32, 3
				}
			}
			for _, c := range []*servertest.WindowedClient{alice, bob} {
				c.MoveToGround(4.5, 64, 2.7, true)
				c.Settle()
				c.UseOnSneaking(id)
				c.ExpectContainerOpen(menu, true, 0, 0, 0)
			}
			diamonds := servertest.ItemStack(t, "minecraft:diamond", 13)
			alice.SetCreativeSlot(36, diamonds)
			alice.Click(hotbar, 0, protocol.ClickModePickup)
			alice.ExpectClickResult(map[int16]protocol.ItemStack{hotbar: {}}, diamonds)
			alice.Click(slot, 0, protocol.ClickModePickup)
			alice.ExpectClickResult(map[int16]protocol.ItemStack{slot: diamonds}, protocol.ItemStack{})
			bob.AwaitWindowSlot(slot, diamonds)

			// Bob takes the stack back out; Alice sees his change too.
			bob.Click(slot, 0, protocol.ClickModePickup)
			bob.ExpectClickResult(map[int16]protocol.ItemStack{slot: {}}, diamonds)
			alice.AwaitWindowSlot(slot, protocol.ItemStack{})

			// A paint drag changes multiple cargo slots through a separate path.
			bob.Click(-999, 0, protocol.ClickModeDrag)
			bob.Drain()
			bob.Click(0, 1, protocol.ClickModeDrag)
			bob.Drain()
			bob.Click(1, 1, protocol.ClickModeDrag)
			bob.Drain()
			bob.Click(-999, 2, protocol.ClickModeDrag)
			six := diamonds
			six.Count = 6
			one := diamonds
			one.Count = 1
			bob.ExpectClickResult(map[int16]protocol.ItemStack{0: six, 1: six}, one)
			alice.AwaitWindowSlot(0, six)
			alice.AwaitWindowSlot(1, six)
		})
	}
}

// vanilla: HopperBlockEntity.ejectItems, AbstractContainerMenu.broadcastChanges
func TestHopperTransferSyncsTwoVehicleViewers(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"minecraft:chest_minecart", "minecraft:oak_chest_boat"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			srv, connect := servertest.Start(t, servertest.BlockTestConfig())
			alice := servertest.WindowedOf(t, "Alice", connect())
			bob := servertest.WindowedOf(t, "Bob", connect())
			servertest.HoldTicks(t, srv)

			if !srv.PlaceContainer(4, 64, 0, "minecraft:hopper") {
				t.Fatal("no hopper")
			}
			srv.SetLoadedBlock(4, 64, 0, registry.MustState("minecraft:hopper", map[string]string{"facing": "south"}))
			if !srv.PlaceContainer(2, 64, 0, "minecraft:chest") {
				t.Fatal("no chest")
			}

			var id int32
			if kind == "minecraft:oak_chest_boat" {
				id = srv.SpawnBoat(4.5, 64, 1.5, kind)
			} else {
				servertest.CartRails(t, srv, 3, 6)
				srv.RunLine("summon " + kind + " 4.5 64.0625 1.5")
				for _, cart := range srv.MinecartsInDimension(world.DimensionName) {
					id = cart.ID
				}
			}

			for _, c := range []*servertest.WindowedClient{alice, bob} {
				c.MoveToGround(4.5, 64, 2.7, true)
				c.Settle()
				c.UseOnSneaking(id)
				c.ExpectContainerOpen(protocol.MenuGeneric9x3, true, 0, 0, 0)
			}

			dirt := servertest.ItemStack(t, "minecraft:dirt", 2)
			srv.SetInventoryAt(4, 64, 0, 0, dirt)
			servertest.RunTicks(t, srv, 8)

			one := servertest.ItemStack(t, "minecraft:dirt", 1)
			alice.AwaitWindowSlot(0, one)
			bob.AwaitWindowSlot(0, one)

			// Bob closes the vehicle menu and opens a separate chest; the next
			// hopper transfer must update only Alice and never send to Bob.
			bob.CloseWindow()
			bob.MoveToGround(2.5, 64, 1.5, true)
			bob.Settle()
			bob.UseBlock(2, 64, 0)
			bob.ExpectContainerOpen(protocol.MenuGeneric9x3, true, 2, 64, 0)
			bob.Drain()

			servertest.RunTicks(t, srv, 8)
			alice.AwaitWindowSlot(0, dirt)
			if got := bob.CollectContainerSlots(); len(got) != 0 {
				t.Fatalf("bob received container slot updates after switching menus: %+v", got)
			}
		})
	}
}
