package entities

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
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

// TestAHopperMinecartTakesTheMouthDropBeforeABodyDrop: one whole item per
// tick, and the mouth is searched before the widened cart box.
// vanilla: MinecartHopper.suckInItems
func TestAHopperMinecartTakesTheMouthDropBeforeABodyDrop(t *testing.T) {
	t.Parallel()
	srv, _ := servertest.OpenWindowed(t, servertest.BlockTestConfig())
	cartID := hopperCartAt(t, srv, "")
	srv.SpawnItemEntity(4.5, 64.25, 1.5, "minecraft:stone", 1)
	srv.SpawnItemEntity(4.5, 65, 1.5, "minecraft:diamond", 1)

	srv.TickMinecarts(1)
	if got := srv.MinecartCargoAt(cartID, 0); got.Count != 1 || got.ItemID != servertest.ItemID(t, "minecraft:diamond") {
		t.Errorf("the cart holds %+v, want the mouth diamond", got)
	}
	items := srv.ItemsInDimension(world.DimensionName)
	if servertest.ItemCountOf(items, "minecraft:stone") != 1 || servertest.ItemCountOf(items, "minecraft:diamond") != 0 {
		t.Errorf("items left %+v, want only the body stone", items)
	}
}

// TestAHopperMinecartFallsBackToABodyDropWhenTheMouthFails: a mouth drop the
// cart has no room for leaves the search to the cart's box.
// vanilla: MinecartHopper.suckInItems
func TestAHopperMinecartFallsBackToABodyDropWhenTheMouthFails(t *testing.T) {
	t.Parallel()
	srv, _ := servertest.OpenWindowed(t, servertest.BlockTestConfig())
	cartID := hopperCartAt(t, srv, "")
	stone := protocol.ItemStack{ItemID: servertest.ItemID(t, "minecraft:stone"), Count: 64}
	for slot := range 4 {
		srv.StowMinecartCargo(cartID, slot, stone)
	}
	stone.Count = 63
	srv.StowMinecartCargo(cartID, 4, stone)
	srv.SpawnItemEntity(4.5, 65, 1.5, "minecraft:diamond", 1)
	srv.SpawnItemEntity(4.5, 64.25, 1.5, "minecraft:stone", 1)

	srv.TickMinecarts(1)
	if got := srv.MinecartCargoAt(cartID, 4); got.Count != 64 {
		t.Errorf("slot 4 holds %+v, want the body stone added", got)
	}
	items := srv.ItemsInDimension(world.DimensionName)
	if servertest.ItemCountOf(items, "minecraft:diamond") != 1 || servertest.ItemCountOf(items, "minecraft:stone") != 0 {
		t.Errorf("items left %+v, want only the diamond", items)
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
