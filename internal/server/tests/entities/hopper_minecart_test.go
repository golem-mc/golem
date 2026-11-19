package entities

import (
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

const hopperCart = "minecraft:hopper_minecart"

// hopperCartAt summons a hopper minecart on the test rails with the tick loop
// held, so only the passes a test drives move it.
func hopperCartAt(t *testing.T, srv *server.Server, tag string) int32 {
	t.Helper()
	srv.StopTick()
	servertest.CartRails(t, srv, 3, 6)
	// The dispatcher refuses input left behind the last argument, so
	// trim the space an empty tag leaves.
	srv.RunLine(strings.TrimSpace("summon " + hopperCart + " 4.5 64.0625 1.5 " + tag))
	for _, cart := range srv.MinecartsInDimension(world.DimensionName) {
		return cart.ID
	}
	t.Fatal("the summon stood no cart")
	return 0
}

func cartSavedTag(t *testing.T, srv *server.Server, id int32) nbt.Compound {
	t.Helper()
	tag, ok := srv.MinecartSaveTag(id)
	if !ok {
		t.Fatal("the cart is gone")
	}
	return tag
}

// TestAHopperMinecartItemStandsAHopperCartOnARail: the item used on a rail
// stands a hopper minecart of the hopper cart entity type.
// vanilla: MinecartItem.useOn
func TestAHopperMinecartItemStandsAHopperCartOnARail(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	srv, c := servertest.OpenWindowed(t, cfg)
	c.MoveToGround(0.5, 64, 0.5, true)
	c.Settle()
	srv.GiveSlot("Testificate", player.HotbarSlot, itemOf(t, hopperCart, 1))
	servertest.CartRails(t, srv, 2, 5)
	c.MoveTo(2.5, 64, 2.9, 0, 60)
	c.Settle()

	c.PlaceBlock(2, 64, 1, protocol.FaceTop)
	typeID, _ := registry.EntityTypeID(hopperCart)
	spawned := servertest.AwaitBoatSpawn(c.Client, typeID)
	if got := srv.MinecartKind(spawned.EntityID); got != hopperCart {
		t.Fatalf("the laid cart is a %q, want %s", got, hopperCart)
	}
}

// TestAHopperMinecartOpensTheFiveSlotMenu: the window is the hopper's five
// slots, titled with the cart's name, and a shift-click fills the first.
// vanilla: MinecartHopper.createMenu
func TestAHopperMinecartOpensTheFiveSlotMenu(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	srv, w := servertest.OpenWindowed(t, cfg)
	w.MoveToGround(0.5, 64, 0.5, true)
	w.Settle()
	cartID := hopperCartAt(t, srv, "")
	srv.GiveSlot("Testificate", player.HotbarSlot, itemOf(t, "minecraft:diamond", 5))
	w.MoveTo(4.5, 64, 2.7, 0, 60)
	w.Settle()

	w.UseOn(cartID)
	content := w.ExpectContainerOpen(protocol.MenuHopper, true, 0, 0, 0)
	if len(content) != 5+36 {
		t.Fatalf("the window shows %d slots, want the cart's 5 and the inventory's 36", len(content))
	}
	// Window slots 32..40 are the hotbar.
	w.Click(32, 0, 1)
	diamond := servertest.MustItemID(t, "minecraft:diamond")
	servertest.PollUntil(t, 10*time.Second, func() bool {
		return srv.MinecartCargoAt(cartID, 0).ItemID == diamond
	})
}

// TestAHopperMinecartPullsOneItemPerTickFromTheContainerAbove: no cooldown
// holds the cart between two pulls, and it pushes nothing back.
// vanilla: MinecartHopper.tryConsumeItems
func TestAHopperMinecartPullsOneItemPerTickFromTheContainerAbove(t *testing.T) {
	t.Parallel()
	srv, _ := servertest.OpenWindowed(t, servertest.BlockTestConfig())
	cartID := hopperCartAt(t, srv, "")
	if !srv.PlaceContainer(4, 65, 1, "minecraft:chest") {
		t.Fatal("no chest")
	}
	srv.SetInventoryAt(4, 65, 1, 0, itemOf(t, "minecraft:diamond", 3))

	srv.TickMinecarts(1)
	if got := srv.MinecartCargoAt(cartID, 0); got.Count != 1 {
		t.Fatalf("after one tick the cart holds %+v, want 1 diamond", got)
	}
	srv.TickMinecarts(1)
	if got := srv.MinecartCargoAt(cartID, 0); got.Count != 2 {
		t.Errorf("after two ticks the cart holds %+v, want 2 diamonds", got)
	}
	if got := srv.InventoryAt(4, 65, 1)[0]; got.Count != 1 {
		t.Errorf("the chest holds %+v, want 1 diamond left", got)
	}
}

// TestAHopperMinecartTakesAnItemEntityWhole: a stack lying in the cart takes
// one tick and leaves the world.
// vanilla: HopperBlockEntity.addItem(Container, ItemEntity)
func TestAHopperMinecartTakesAnItemEntityWhole(t *testing.T) {
	t.Parallel()
	srv, _ := servertest.OpenWindowed(t, servertest.BlockTestConfig())
	cartID := hopperCartAt(t, srv, "")
	srv.SpawnItemEntity(4.5, 65, 1.5, "minecraft:diamond", 3)

	srv.TickMinecarts(1)
	if got := srv.MinecartCargoAt(cartID, 0); got.Count != 3 {
		t.Errorf("the cart holds %+v, want the 3 diamonds", got)
	}
	if left := servertest.ItemCountOf(srv.ItemsInDimension(world.DimensionName), "minecraft:diamond"); left != 0 {
		t.Errorf("%d diamonds still lie there", left)
	}
}

// TestAHopperMinecartLeavesAnItemOutOfItsReach: a stack three blocks off is
// not pulled.
func TestAHopperMinecartLeavesAnItemOutOfItsReach(t *testing.T) {
	t.Parallel()
	srv, _ := servertest.OpenWindowed(t, servertest.BlockTestConfig())
	cartID := hopperCartAt(t, srv, "")
	srv.SpawnItemEntity(4.5, 65, 4.5, "minecraft:diamond", 3)

	srv.TickMinecarts(1)
	if got := srv.MinecartCargoAt(cartID, 0); !got.Empty() {
		t.Errorf("the cart holds %+v, want nothing", got)
	}
}

// TestAnActivatorRailSwitchesAHopperMinecart: a powered activator disables
// the pull, an unpowered one enables it again.
// vanilla: MinecartHopper.activateMinecart
func TestAnActivatorRailSwitchesAHopperMinecart(t *testing.T) {
	t.Parallel()
	srv, _ := servertest.OpenWindowed(t, servertest.BlockTestConfig())
	cartID := hopperCartAt(t, srv, "")
	activator := func(powered string) {
		t.Helper()
		if !srv.SetBlock(4, 64, 1, servertest.MustState(t, "minecraft:activator_rail",
			map[string]string{"shape": "east_west", "powered": powered})) {
			t.Fatal("the activator did not stand")
		}
	}
	enabled := func() bool {
		v, _ := nbt.Get[nbt.Byte](cartSavedTag(t, srv, cartID), "Enabled")
		return v != 0
	}
	if !enabled() {
		t.Fatal("a fresh hopper cart is disabled")
	}
	activator("true")
	srv.TickMinecarts(1)
	if enabled() {
		t.Fatal("a powered activator left the cart enabled")
	}
	srv.SpawnItemEntity(4.5, 65, 1.5, "minecraft:diamond", 3)
	srv.TickMinecarts(1)
	if got := srv.MinecartCargoAt(cartID, 0); !got.Empty() {
		t.Errorf("the disabled cart pulled %+v", got)
	}
	activator("false")
	srv.TickMinecarts(1)
	if !enabled() {
		t.Error("an unpowered activator left the cart disabled")
	}
	if got := srv.MinecartCargoAt(cartID, 0); got.Count != 3 {
		t.Errorf("the re-enabled cart holds %+v, want the 3 diamonds", got)
	}
}

// TestABrokenHopperMinecartDropsItselfAndItsContents: a broken hopper cart
// leaves a hopper minecart item and its stacks.
// vanilla: MinecartHopper.getDropItem
func TestABrokenHopperMinecartDropsItselfAndItsContents(t *testing.T) {
	t.Parallel()
	srv, _ := servertest.OpenWindowed(t, servertest.BlockTestConfig())
	cartID := hopperCartAt(t, srv, "")
	srv.StowMinecartCargo(cartID, 4, itemOf(t, "minecraft:emerald", 7))

	srv.RunLine("kill @e[type=" + hopperCart + "]")
	servertest.PollUntil(t, 10*time.Second, func() bool {
		srv.TickMinecarts(1)
		return srv.MinecartKind(cartID) == ""
	})
	if got := servertest.ItemCountOf(srv.ItemsInDimension(world.DimensionName), "minecraft:emerald"); got != 7 {
		t.Errorf("%d emeralds spilled, want 7", got)
	}
}

// TestAHopperMinecartSavesItemsAndEnabled: the saved cart carries Items and
// Enabled, and a summon with Enabled:0b starts switched off.
// vanilla: MinecartHopper.addAdditionalSaveData
func TestAHopperMinecartSavesItemsAndEnabled(t *testing.T) {
	t.Parallel()
	srv, _ := servertest.OpenWindowed(t, servertest.BlockTestConfig())
	cartID := hopperCartAt(t, srv, `{Enabled:0b,Items:[{Slot:3b,id:"minecraft:stick",count:2}]}`)

	tag := cartSavedTag(t, srv, cartID)
	if v, ok := nbt.Get[nbt.Byte](tag, "Enabled"); !ok || v != 0 {
		t.Errorf("saved Enabled = %v, %v; want 0b", v, ok)
	}
	items, _ := nbt.Get[nbt.List](tag, "Items")
	if len(items.Items) != 1 {
		t.Fatalf("saved %d stacks, want 1", len(items.Items))
	}
	if got := srv.MinecartCargoAt(cartID, 3); got.Count != 2 {
		t.Errorf("slot 3 holds %+v, want 2 sticks", got)
	}
	srv.SpawnItemEntity(4.5, 65, 1.5, "minecraft:diamond", 3)
	srv.TickMinecarts(1)
	if got := srv.MinecartCargoAt(cartID, 0); !got.Empty() {
		t.Errorf("a cart summoned disabled pulled %+v", got)
	}
}

// TestAHopperMinecartReadsEnabledFromAnyNumericTag: {Enabled:0} (an Int)
// switches the cart off, and a cart summoned without Enabled still pulls.
// vanilla: MinecartHopper.readAdditionalSaveData
func TestAHopperMinecartReadsEnabledFromAnyNumericTag(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		tag  string
		pull bool
	}{{`{Enabled:0}`, false}, {"", true}} {
		srv, _ := servertest.OpenWindowed(t, servertest.BlockTestConfig())
		cartID := hopperCartAt(t, srv, tc.tag)
		if !srv.PlaceContainer(4, 65, 1, "minecraft:chest") {
			t.Fatal("no chest")
		}
		srv.SetInventoryAt(4, 65, 1, 0, itemOf(t, "minecraft:diamond", 64))
		srv.TickMinecarts(10)
		if got := srv.MinecartCargoAt(cartID, 0); got.Empty() == tc.pull {
			t.Errorf("summoned with %q, after 10 ticks the cart holds %+v; want pulling %v", tc.tag, got, tc.pull)
		}
	}
}

// extraCartAt summons a cart of a kind at a position and returns the ID it
// got, whichever carts stood before.
func extraCartAt(t *testing.T, srv *server.Server, kind string, x, y, z float64, tag string) int32 {
	t.Helper()
	known := map[int32]bool{}
	for _, cart := range srv.MinecartsInDimension(world.DimensionName) {
		known[cart.ID] = true
	}
	srv.RunLine(strings.TrimSpace(fmt.Sprintf("summon %s %.4f %.4f %.4f %s", kind, x, y, z, tag)))
	for _, cart := range srv.MinecartsInDimension(world.DimensionName) {
		if !known[cart.ID] {
			return cart.ID
		}
	}
	t.Fatal("the summon stood no cart")
	return 0
}

// TestAHopperMinecartPullsFromAChestCartAbove: a container cart in the block
// above is a container to pull from.
// vanilla: HopperBlockEntity.getEntityContainer
func TestAHopperMinecartPullsFromAChestCartAbove(t *testing.T) {
	t.Parallel()
	srv, _ := servertest.OpenWindowed(t, servertest.BlockTestConfig())
	srv.StopTick()
	// Off the rails: a cart over a rail snaps down onto it.
	hopperID := extraCartAt(t, srv, hopperCart, 12.5, 64, 1.5, "")
	chestID := extraCartAt(t, srv, "minecraft:chest_minecart", 12.5, 65.0625, 1.5,
		`{Items:[{Slot:0b,id:"minecraft:diamond",count:3}]}`)

	srv.TickMinecarts(1)
	if got := srv.MinecartCargoAt(hopperID, 0); got.Count != 1 {
		t.Errorf("the hopper cart holds %+v, want 1 diamond", got)
	}
	if got := srv.MinecartCargoAt(chestID, 0); got.Count != 2 {
		t.Errorf("the chest cart holds %+v, want 2 diamonds left", got)
	}
}

// TestAHopperBlockPullsFromAChestCartAbove: the entity fallback serves the
// block hopper too.
// vanilla: HopperBlockEntity.getContainerAt
func TestAHopperBlockPullsFromAChestCartAbove(t *testing.T) {
	t.Parallel()
	srv, _ := servertest.OpenWindowed(t, servertest.BlockTestConfig())
	srv.StopTick()
	if !srv.PlaceContainer(10, 64, 1, "minecraft:hopper") {
		t.Fatal("no hopper")
	}
	chestID := extraCartAt(t, srv, "minecraft:chest_minecart", 10.5, 65.0625, 1.5,
		`{Items:[{Slot:0b,id:"minecraft:diamond",count:3}]}`)

	servertest.TickBlockEntitiesByHand(srv, 1)
	if got := srv.InventoryAt(10, 64, 1)[0]; got.Count != 1 {
		t.Errorf("the hopper holds %+v, want 1 diamond", got)
	}
	if got := srv.MinecartCargoAt(chestID, 0); got.Count != 2 {
		t.Errorf("the chest cart holds %+v, want 2 diamonds left", got)
	}
}

// TestAHopperBlockPushesIntoAChestCartBelow: a hopper facing down fills the
// container cart standing in the block below.
// vanilla: HopperBlockEntity.ejectItems
func TestAHopperBlockPushesIntoAChestCartBelow(t *testing.T) {
	t.Parallel()
	srv, _ := servertest.OpenWindowed(t, servertest.BlockTestConfig())
	srv.StopTick()
	if !srv.PlaceContainer(10, 65, 1, "minecraft:hopper") {
		t.Fatal("no hopper")
	}
	srv.SetInventoryAt(10, 65, 1, 0, itemOf(t, "minecraft:diamond", 3))
	chestID := extraCartAt(t, srv, "minecraft:chest_minecart", 10.5, 64.0625, 1.5, "")

	servertest.TickBlockEntitiesByHand(srv, 1)
	if got := srv.MinecartCargoAt(chestID, 0); got.Count != 1 {
		t.Errorf("the chest cart holds %+v, want 1 diamond", got)
	}
	if got := srv.InventoryAt(10, 65, 1)[0]; got.Count != 2 {
		t.Errorf("the hopper holds %+v, want 2 diamonds left", got)
	}
}

// TestAHopperMinecartTopsUpAPartialStack: an item entity larger than the
// room left is taken in part and keeps the rest.
// vanilla: HopperBlockEntity.addItem(Container, ItemEntity)
func TestAHopperMinecartTopsUpAPartialStack(t *testing.T) {
	t.Parallel()
	srv, _ := servertest.OpenWindowed(t, servertest.BlockTestConfig())
	cartID := hopperCartAt(t, srv, "")
	for slot := range 5 {
		srv.StowMinecartCargo(cartID, slot, itemOf(t, "minecraft:stick", 64))
	}
	srv.StowMinecartCargo(cartID, 2, itemOf(t, "minecraft:diamond", 60))
	srv.SpawnItemEntity(4.5, 65, 1.5, "minecraft:diamond", 10)

	srv.TickMinecarts(1)
	if got := srv.MinecartCargoAt(cartID, 2); got.Count != 64 {
		t.Errorf("slot 2 holds %+v, want 64 diamonds", got)
	}
	if left := servertest.ItemCountOf(srv.ItemsInDimension(world.DimensionName), "minecraft:diamond"); left != 6 {
		t.Errorf("%d diamonds still lie there, want 6", left)
	}
}

// TestAFullHopperMinecartTakesNothing: with no room, neither an item entity
// nor the container above gives anything up.
func TestAFullHopperMinecartTakesNothing(t *testing.T) {
	t.Parallel()
	srv, _ := servertest.OpenWindowed(t, servertest.BlockTestConfig())
	cartID := hopperCartAt(t, srv, "")
	for slot := range 5 {
		srv.StowMinecartCargo(cartID, slot, itemOf(t, "minecraft:stick", 64))
	}
	if !srv.PlaceContainer(4, 65, 1, "minecraft:chest") {
		t.Fatal("no chest")
	}
	srv.SetInventoryAt(4, 65, 1, 0, itemOf(t, "minecraft:diamond", 3))
	srv.SpawnItemEntity(4.5, 65, 1.5, "minecraft:emerald", 3)

	srv.TickMinecarts(1)
	if got := srv.InventoryAt(4, 65, 1)[0]; got.Count != 3 {
		t.Errorf("the chest holds %+v, want its 3 diamonds", got)
	}
	if left := servertest.ItemCountOf(srv.ItemsInDimension(world.DimensionName), "minecraft:emerald"); left != 3 {
		t.Errorf("%d emeralds lie there, want 3", left)
	}
}

// TestAHopperMinecartPullsOnlyAFurnaceOutput: from a furnace above, only the
// result slot can be taken.
// vanilla: AbstractFurnaceBlockEntity.canTakeItemThroughFace
func TestAHopperMinecartPullsOnlyAFurnaceOutput(t *testing.T) {
	t.Parallel()
	srv, _ := servertest.OpenWindowed(t, servertest.BlockTestConfig())
	cartID := hopperCartAt(t, srv, "")
	if !srv.PlaceContainer(4, 65, 1, "minecraft:furnace") {
		t.Fatal("no furnace")
	}
	srv.SetInventoryAt(4, 65, 1, 0, itemOf(t, "minecraft:diamond", 3))
	srv.SetInventoryAt(4, 65, 1, 2, itemOf(t, "minecraft:iron_ingot", 2))

	srv.TickMinecarts(1)
	if got := srv.MinecartCargoAt(cartID, 0); got.Count != 1 || got.ItemID != servertest.MustItemID(t, "minecraft:iron_ingot") {
		t.Errorf("the cart holds %+v, want 1 iron ingot", got)
	}
	if got := srv.InventoryAt(4, 65, 1)[0]; got.Count != 3 {
		t.Errorf("the furnace input is %+v, want it untouched", got)
	}
}

// TestAHopperMinecartSearchesAroundItsOwnPosition: the hopper cart's pull
// looks for a container cart in the box centred on the cart plus one block up,
// not in the block cell, so a cart beyond the cell edge is still reached.
// vanilla: HopperBlockEntity.getSourceContainer
func TestAHopperMinecartSearchesAroundItsOwnPosition(t *testing.T) {
	t.Parallel()
	srv, _ := servertest.OpenWindowed(t, servertest.BlockTestConfig())
	srv.StopTick()
	hopperID := extraCartAt(t, srv, hopperCart, 12.9, 64, 1.5, "")
	chestID := extraCartAt(t, srv, "minecraft:chest_minecart", 13.6, 65.0625, 1.5,
		`{Items:[{Slot:0b,id:"minecraft:diamond",count:3}]}`)

	srv.TickMinecarts(1)
	if got := srv.MinecartCargoAt(hopperID, 0); got.Count != 1 {
		t.Errorf("the hopper cart holds %+v, want 1 diamond", got)
	}
	if got := srv.MinecartCargoAt(chestID, 0); got.Count != 2 {
		t.Errorf("the chest cart holds %+v, want 2 diamonds left", got)
	}
}

// vanilla: Minecart.activateMinecart, MinecartHopper.activateMinecart
func TestActivatorRailsShakeOnlyRideableMinecarts(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"minecraft:minecart", hopperCart, "minecraft:chest_minecart", "minecraft:furnace_minecart", "minecraft:tnt_minecart"} {
		t.Run(kind, func(t *testing.T) {
			cfg := servertest.BlockTestConfig()
			cfg.GameMode = "survival"
			srv, connect := servertest.Start(t, cfg)
			c := connect().Join("Carter")
			c.MoveToGround(4.5, 64, 2.7, true)
			c.Settle()
			servertest.HoldTicks(t, srv)
			servertest.CartRails(t, srv, 3, 6)
			id := extraCartAt(t, srv, kind, 4.5, 64.0625, 1.5, "")
			r := servertest.NewMetadataRecorder(t, srv, c)
			r.Read(1) // Drain spawn metadata before powering the rail.
			litActivator(t, srv, 4, "true")
			r.Record("powered", id, r.Read(20))
			shook := false
			for _, payload := range r.Got["powered"] {
				indices := metadataIndices(t, payload)
				if slices.Contains(indices, protocol.MetadataIndexVehicleShake) || slices.Contains(indices, protocol.MetadataIndexVehicleShakeDamage) {
					shook = true
				}
			}
			if want := kind == "minecraft:minecart"; shook != want {
				t.Errorf("hurt time or damage metadata sent = %v, want %v", shook, want)
			}
			if kind != hopperCart {
				return
			}
			if enabled := nbt.BoolField(cartSavedTag(t, srv, id), "Enabled"); enabled {
				t.Error("powered activator left the hopper enabled")
			}
			c.Attack(id)
			packets := r.Read(1)
			if srv.MinecartKind(id) != hopperCart {
				t.Fatal("one bare-hand survival punch removed the hopper cart")
			}
			r.Record("punched", id, packets)
			if len(r.Got["punched"]) == 0 {
				t.Error("the punch sent no hurt metadata")
			}
		})
	}
}
