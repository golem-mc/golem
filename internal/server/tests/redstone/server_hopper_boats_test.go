package redstone

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

func TestAHopperDrainsAChestBoat(t *testing.T) {
	t.Parallel()
	srv, _ := servertest.OpenWindowed(t, servertest.BlockTestConfig())
	servertest.HoldTicks(t, srv)
	if !srv.PlaceContainer(4, 64, 0, "minecraft:hopper") {
		t.Fatal("no hopper")
	}
	boat := srv.SpawnBoat(4.5, 65, 0.5, "minecraft:oak_chest_boat")
	srv.StowBoatCargo(boat, 0, servertest.ItemStack(t, "minecraft:cobblestone", 5))
	for n := int32(1); n <= 5; n++ {
		servertest.RunTicks(t, srv, 8)
		if got := srv.InventoryAt(4, 64, 0)[0]; got.Count != n || registry.Item(got.ItemID) != registry.ItemCobblestone {
			t.Fatalf("after %d ticks hopper holds %+v, want %d cobblestone", n*8, got, n)
		}
		if got := srv.BoatCargoAt(boat, 0); got.Count != 5-n {
			t.Fatalf("boat holds %+v, want %d left", got, 5-n)
		}
	}
}

func TestADropperFeedsAChestBoat(t *testing.T) {
	t.Parallel()
	srv, _ := servertest.OpenWindowed(t, servertest.BlockTestConfig())
	servertest.HoldTicks(t, srv)
	if !srv.PlaceContainer(4, 64, 0, "minecraft:dropper") {
		t.Fatal("no dropper")
	}
	boat := srv.SpawnBoat(4.5, 64, 1.5, "minecraft:oak_chest_boat")
	dirt := servertest.ItemStack(t, "minecraft:dirt", 1)
	srv.SetInventoryAt(4, 64, 0, 0, dirt)
	srv.Dispense(4, 64, 0, protocol.FaceSouth)
	if got := srv.BoatCargoAt(boat, 0); !got.Equal(dirt) {
		t.Errorf("boat holds %+v, want dirt", got)
	}
	if !servertest.NothingIn(srv.InventoryAt(4, 64, 0)) {
		t.Error("dropper kept the dirt")
	}
	if got := srv.ItemsInDimension(world.DimensionName); len(got) != 0 {
		t.Errorf("dropper spawned items: %+v", got)
	}
}

func TestAHopperFeedsAChestBoat(t *testing.T) {
	t.Parallel()
	srv, _ := servertest.OpenWindowed(t, servertest.BlockTestConfig())
	servertest.HoldTicks(t, srv)
	if !srv.PlaceContainer(4, 64, 0, "minecraft:hopper") {
		t.Fatal("no hopper")
	}
	srv.SetLoadedBlock(4, 64, 0, registry.MustState("minecraft:hopper", map[string]string{"facing": "south"}))
	boat := srv.SpawnBoat(4.5, 64, 1.5, "minecraft:bamboo_chest_raft")
	dirt := servertest.ItemStack(t, "minecraft:dirt", 1)
	srv.SetInventoryAt(4, 64, 0, 0, dirt)
	servertest.RunTicks(t, srv, 8)
	if got := srv.BoatCargoAt(boat, 0); !got.Equal(dirt) {
		t.Errorf("boat holds %+v, want dirt", got)
	}
	if !servertest.NothingIn(srv.InventoryAt(4, 64, 0)) {
		t.Error("hopper kept the dirt")
	}
}

func TestAHopperMinecartDrainsAChestBoat(t *testing.T) {
	t.Parallel()
	srv, _ := servertest.OpenWindowed(t, servertest.BlockTestConfig())
	servertest.HoldTicks(t, srv)
	servertest.CartRails(t, srv, 3, 6)
	cart := srv.SpawnMinecartOfKind("minecraft:hopper_minecart", 4.5, 64.0625, 1.5)
	srv.RunLine(`summon minecraft:oak_chest_boat 4.5 65.5 1.5 {NoGravity:1b,Items:[{Slot:0b,id:"minecraft:cobblestone",count:3}]}`)
	servertest.RunTicks(t, srv, 3)
	if got := srv.MinecartCargoAt(cart, 0); got.Count != 3 || registry.Item(got.ItemID) != registry.ItemCobblestone {
		t.Errorf("cart holds %+v, want 3 cobblestone", got)
	}
}

func TestADropperChoosesBetweenChestBoatsAndCarts(t *testing.T) {
	t.Parallel()
	srv, _ := servertest.OpenWindowed(t, servertest.BlockTestConfig())
	servertest.HoldTicks(t, srv)
	if !srv.PlaceContainer(4, 64, 0, "minecraft:dropper") {
		t.Fatal("no dropper")
	}
	boat := srv.SpawnBoat(4.5, 64, 1.5, "minecraft:oak_chest_boat")
	cart := srv.SpawnMinecartOfKind("minecraft:chest_minecart", 4.5, 64, 1.5)
	srv.SetInventoryAt(4, 64, 0, 0, servertest.ItemStack(t, "minecraft:dirt", 64))
	for range 64 {
		srv.Dispense(4, 64, 0, protocol.FaceSouth)
	}
	b, c := srv.BoatCargoAt(boat, 0).Count, srv.MinecartCargoAt(cart, 0).Count
	if b == 0 || c == 0 || b+c != 64 {
		t.Fatalf("boat/cart received %d/%d dirt, want both to share all 64", b, c)
	}
}

func TestAHopperUnpacksAChestBoatsLoot(t *testing.T) {
	t.Parallel()
	srv, _ := servertest.OpenWindowed(t, servertest.BlockTestConfig())
	servertest.HoldTicks(t, srv)
	if !srv.PlaceContainer(4, 64, 0, "minecraft:hopper") {
		t.Fatal("no hopper")
	}
	srv.RunLine(`summon minecraft:oak_chest_boat 4.5 65 0.5 {LootTable:"minecraft:chests/spawn_bonus_chest",LootTableSeed:7L}`)
	servertest.RunTicks(t, srv, 8)
	if servertest.NothingIn(srv.InventoryAt(4, 64, 0)) {
		t.Fatal("hopper took nothing from the pending loot table")
	}
}

func TestADropperUpdatesAnOpenChestBoatWindow(t *testing.T) {
	t.Parallel()
	srv, c := servertest.OpenWindowed(t, servertest.BlockTestConfig())
	servertest.HoldTicks(t, srv)
	c.MoveToGround(4.5, 64, 3.5, true)
	c.Settle()
	if !srv.PlaceContainer(4, 64, 0, "minecraft:dropper") {
		t.Fatal("no dropper")
	}
	boat := srv.SpawnBoat(4.5, 64, 1.5, "minecraft:oak_chest_boat")
	typeID, _ := registry.EntityTypeID("minecraft:oak_chest_boat")
	servertest.AwaitBoatSpawn(c.Client, typeID)
	c.UseOnSneaking(boat)
	c.ExpectContainerOpen(protocol.MenuGeneric9x3, true, 0, 0, 0)
	dirt := servertest.ItemStack(t, "minecraft:dirt", 1)
	srv.SetInventoryAt(4, 64, 0, 0, dirt)
	srv.Dispense(4, 64, 0, protocol.FaceSouth)
	c.AwaitWindowSlot(0, dirt)
}

func TestAFrozenSteppedDropperUpdatesAnOpenChestBoatWindow(t *testing.T) {
	t.Parallel()
	for _, trigger := range []string{"setblock", "SetBlockAndTell"} {
		t.Run(trigger, func(t *testing.T) {
			t.Parallel()
			srv, c := servertest.OpenWindowedOps(t, servertest.BlockTestConfig())
			servertest.HoldTicks(t, srv)
			c.ChatCommand("tick freeze")
			c.ExpectTickingState(20, true)
			c.ExpectCommandFeedback("commands.tick.status.frozen")

			srv.RunLine("fill 6 99 -2 11 99 5 minecraft:stone")
			srv.RunLine("setblock 8 100 0 minecraft:dropper[facing=south]")
			srv.RunLine("setblock 9 100 0 minecraft:lever[face=floor]")
			srv.RunLine("item replace block 8 100 0 container.0 with minecraft:dirt 1")
			c.MoveToGround(8.5, 100, 3.5, true)
			c.Settle()
			srv.RunLine(`summon minecraft:oak_chest_boat 8.5 100 1.5 {NoGravity:1b}`)
			boats := srv.BoatIDs()
			if len(boats) != 1 {
				t.Fatalf("spawned %d boats, want 1", len(boats))
			}
			boat := boats[0]
			typeID, _ := registry.EntityTypeID("minecraft:oak_chest_boat")
			servertest.AwaitBoatSpawn(c.Client, typeID)
			c.UseOnSneaking(boat)
			cargo := c.ExpectContainerOpen(protocol.MenuGeneric9x3, true, 0, 0, 0)
			if !servertest.NothingIn(cargo) {
				t.Fatalf("boat opened with cargo: %+v", cargo)
			}
			dirt := servertest.ItemStack(t, "minecraft:dirt", 1)
			if got := srv.InventoryAt(8, 100, 0)[0]; !got.Equal(dirt) {
				t.Fatalf("dropper holds %+v, want dirt", got)
			}

			if trigger == "setblock" {
				c.ChatCommand("setblock 9 100 0 minecraft:redstone_block")
				c.ExpectCommandFeedback("commands.setblock.success", "9", "100", "0")
			} else {
				srv.SetBlockAndTell(9, 100, 0, registry.BlockRedstoneBlock.DefaultState())
			}
			age, _ := srv.Time()
			servertest.RunTicks(t, srv, 8)
			if got := srv.InventoryAt(8, 100, 0)[0]; !got.Equal(dirt) {
				t.Fatalf("frozen dropper spent its dirt: %+v", got)
			}
			if got := srv.BoatCargoAt(boat, 0); !got.Empty() {
				t.Fatalf("frozen boat received %+v", got)
			}

			c.ChatCommand("tick step 8")
			c.ExpectTickingStep(8)
			eight := int64(8)
			c.ExpectCommandFeedbackText("commands.tick.step.success", protocol.Text{Number: &eight})
			servertest.RunTicks(t, srv, 8)
			c.AwaitWindowSlot(0, dirt)
			if got := srv.BoatCargoAt(boat, 0); !got.Equal(dirt) {
				t.Errorf("boat holds %+v, want dirt", got)
			}
			if !servertest.NothingIn(srv.InventoryAt(8, 100, 0)) {
				t.Error("dropper kept the dirt")
			}
			if got := srv.ItemsInDimension(world.DimensionName); len(got) != 0 {
				t.Errorf("dropper spawned items instead of transferring: %+v", got)
			}
			servertest.RunTicks(t, srv, 8)
			if at, _ := srv.Time(); at != age+8 {
				t.Errorf("game advanced %d ticks, want 8", at-age)
			}
		})
	}
}
