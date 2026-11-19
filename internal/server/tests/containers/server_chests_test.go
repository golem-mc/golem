package containers

// The double and the trapped chest: two chests side by side pair into one
// window of 54 over both halves, a hopper at either half sees the pair
// whole, the pair's lid and sound count across both halves, and a trapped
// chest powers redstone with the count of players looking in.

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/registry"
)

// chestPropsOf reads the properties of the block at a position, failing
// unless it is the block named.
func chestPropsOf(t *testing.T, srv *server.Server, x, y, z int32, want string) map[string]string {
	t.Helper()
	name, props := servertest.StatePropsOf(t, srv, x, y, z)
	if name != want {
		t.Fatalf("the block at (%d, %d, %d) is %s, want %s", x, y, z, name, want)
	}
	return props
}

// TestTwoChestsPairIntoOneWindowAndSplitWhenOneBreaks checks the pair's
// life: the second chest takes the right half and turns the first into the
// left, either half opens six rows over both - the right half's 27 first,
// vanilla's CompoundContainer order - and breaking one half leaves the other
// a single with what it held.
func TestTwoChestsPairIntoOneWindowAndSplitWhenOneBreaks(t *testing.T) {
	t.Parallel()
	srv, w := servertest.OpenWindowed(t, servertest.BlockTestConfig())

	servertest.PlaceChestPair(t, w, "minecraft:chest", 4, 64, 0)
	if props := chestPropsOf(t, srv, 4, 64, 0, "minecraft:chest"); props["facing"] != "north" || props["type"] != "left" {
		t.Errorf("the first chest stands %v, want north left", props)
	}
	if props := chestPropsOf(t, srv, 5, 64, 0, "minecraft:chest"); props["facing"] != "north" || props["type"] != "right" {
		t.Errorf("the second chest stands %v, want north right", props)
	}

	w.UseBlock(4, 64, 0)
	if slots := w.ExpectContainerOpen(protocol.MenuGeneric9x6, true, 4, 64, 0); len(slots) != 54+36 {
		t.Fatalf("the double window has %d slots, want the pair's 54 and the player's 36", len(slots))
	}

	// The hotbar's first slot is window slot 81, behind the pair's 54 and
	// the player's main 27.
	diamonds := servertest.ItemStack(t, "minecraft:diamond", 8)
	dirt := servertest.ItemStack(t, "minecraft:dirt", 8)
	w.SetCreativeSlot(36, diamonds)
	w.Click(81, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{81: {}}, diamonds)
	w.Click(0, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{0: diamonds}, protocol.ItemStack{})
	w.SetCreativeSlot(36, dirt)
	w.Click(81, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{81: {}}, dirt)
	w.Click(27, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{27: dirt}, protocol.ItemStack{})
	if got := srv.InventoryAt(5, 64, 0); len(got) == 0 || !got[0].Equal(diamonds) {
		t.Errorf("the right half holds %+v, want the diamonds in its slot 0", got)
	}
	if got := srv.InventoryAt(4, 64, 0); len(got) == 0 || !got[0].Equal(dirt) {
		t.Errorf("the left half holds %+v, want the dirt in its slot 0", got)
	}
	w.CloseWindow()
	w.ExpectWindowZero()

	w.SetCreativeSlot(36, servertest.ItemStack(t, "minecraft:stone_pickaxe", 1))
	w.BreakBlock(5, 64, 0)
	w.AwaitChestType(4, 64, 0, "single")
	if props := chestPropsOf(t, srv, 4, 64, 0, "minecraft:chest"); props["type"] != "single" {
		t.Errorf("the left half stands %v after its partner broke, want single", props)
	}
	if got := srv.InventoryAt(4, 64, 0); len(got) != 27 || !got[0].Equal(dirt) {
		t.Errorf("the single chest holds %+v, want its 27 slots with the dirt kept", got)
	}
}

// TestSneakingAgainstAChestsSidePairsWithIt checks the sneaking placement:
// against a single chest's side the new chest takes that chest's facing and
// pairs with it, against its front it stays a single, and a trapped chest
// never pairs with a plain one.
func TestSneakingAgainstAChestsSidePairsWithIt(t *testing.T) {
	t.Parallel()
	srv, w := servertest.OpenWindowed(t, servertest.BlockTestConfig())

	w.FacePlayer(4.5, 64, 3.5, 0, 0)
	w.SetCreativeSlot(36, servertest.ItemStack(t, "minecraft:chest", 3))
	w.PlaceBlock(4, 63, 0, protocol.FaceTop)
	w.ExpectBlockAck()

	// Against the front: a single of its own.
	w.Sneak(true)
	w.PlaceBlock(4, 64, 0, protocol.FaceNorth)
	w.ExpectBlockAck()
	if props := chestPropsOf(t, srv, 4, 64, -1, "minecraft:chest"); props["type"] != "single" {
		t.Errorf("the chest at the front stands %v, want single", props)
	}

	// Against the east side, looking another way: the clicked chest's
	// facing, and the right half of the pair.
	w.FacePlayer(4.5, 64, 3.5, 90, 0)
	w.PlaceBlock(4, 64, 0, protocol.FaceEast)
	w.ExpectBlockAck()
	if props := chestPropsOf(t, srv, 5, 64, 0, "minecraft:chest"); props["facing"] != "north" || props["type"] != "right" {
		t.Errorf("the chest at the side stands %v, want north right", props)
	}
	if props := chestPropsOf(t, srv, 4, 64, 0, "minecraft:chest"); props["facing"] != "north" || props["type"] != "left" {
		t.Errorf("the clicked chest stands %v, want north left", props)
	}
	if props := chestPropsOf(t, srv, 4, 64, -1, "minecraft:chest"); props["type"] != "single" {
		t.Errorf("the chest at the front stands %v after the pair formed, want single", props)
	}

	// A trapped chest beside the front chest, standing: no pair.
	w.Sneak(false)
	w.FacePlayer(4.5, 64, 3.5, 0, 0)
	w.SetCreativeSlot(36, servertest.ItemStack(t, "minecraft:trapped_chest", 1))
	w.PlaceBlock(3, 63, -1, protocol.FaceTop)
	w.ExpectBlockAck()
	if props := chestPropsOf(t, srv, 3, 64, -1, "minecraft:trapped_chest"); props["type"] != "single" {
		t.Errorf("the trapped chest stands %v beside a plain one, want single", props)
	}

	w.UseBlock(5, 64, 0)
	if slots := w.ExpectContainerOpen(protocol.MenuGeneric9x6, true, 5, 64, 0); len(slots) != 90 {
		t.Errorf("the pair's window has %d slots, want 90", len(slots))
	}
}

// TestAHopperSeesADoubleChestWhole checks the hoppers at a pair: one under
// the right half drains what only the left half holds, and one on top of
// the left half fills the right half's first slot, both reading the pair as
// its 54 slots, right half first.
func TestAHopperSeesADoubleChestWhole(t *testing.T) {
	t.Parallel()
	srv, w := servertest.OpenWindowed(t, servertest.BlockTestConfig())

	servertest.PlaceChestPair(t, w, "minecraft:chest", 4, 64, 0)
	w.SetCreativeSlot(36, servertest.ItemStack(t, "minecraft:hopper", 1))
	w.Sneak(true)
	w.PlaceBlock(4, 64, 0, protocol.FaceTop)
	w.Sneak(false)
	w.ExpectBlockAck()
	if name, props := servertest.StatePropsOf(t, srv, 4, 65, 0); name != "minecraft:hopper" || props["facing"] != "down" {
		t.Fatalf("the upper hopper stands as %s %v, want a hopper facing down", name, props)
	}
	w.SetCreativeSlot(36, servertest.ItemStack(t, "minecraft:stone_pickaxe", 1))
	w.BreakBlock(5, 63, 0)
	w.ExpectBlockAck()
	w.SetCreativeSlot(36, servertest.ItemStack(t, "minecraft:hopper", 1))
	w.Sneak(true)
	w.PlaceBlock(5, 64, 0, protocol.FaceBottom)
	w.Sneak(false)
	w.ExpectBlockAck()
	if name, _ := servertest.StatePropsOf(t, srv, 5, 63, 0); name != "minecraft:hopper" {
		t.Fatalf("the lower hopper stands as %s, want a hopper", name)
	}
	srv.StopTick()

	stick := servertest.ItemStack(t, "minecraft:stick", 1)
	if !srv.SetInventoryAt(4, 64, 0, 0, stick) {
		t.Fatal("the left half has no block entity to fill")
	}
	servertest.TickBlockEntitiesByHand(srv, 1)
	if got := srv.InventoryAt(5, 63, 0); len(got) == 0 || !got[0].Equal(stick) {
		t.Errorf("the hopper under the right half holds %+v, want the left half's stick", got)
	}
	if got := srv.InventoryAt(4, 64, 0); !servertest.NothingIn(got) {
		t.Errorf("the left half holds %+v after the drain, want nothing", got)
	}

	// The lower hopper is on its cooldown now, so what moves next is the
	// upper hopper's push alone.
	dirt := servertest.ItemStack(t, "minecraft:dirt", 1)
	if !srv.SetInventoryAt(4, 65, 0, 0, dirt) {
		t.Fatal("the upper hopper has no block entity to fill")
	}
	servertest.TickBlockEntitiesByHand(srv, 1)
	if got := srv.InventoryAt(5, 64, 0); len(got) == 0 || !got[0].Equal(dirt) {
		t.Errorf("the right half holds %+v, want the dirt the hopper on the left half pushed", got)
	}
	if got := srv.InventoryAt(4, 64, 0); !servertest.NothingIn(got) {
		t.Errorf("the left half holds %+v, want nothing: the pair fills from the right half", got)
	}
}

// TestATrappedChestPowersRedstoneWhileOpen checks the trapped chest: its
// own block entity and lid block, and a signal of as many players as look
// in, which a wire beside it carries.
func TestATrappedChestPowersRedstoneWhileOpen(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.BlockTestConfig())

	alice := servertest.WindowedOf(t, "Alice", connect())
	alice.FacePlayer(4.5, 64, 3.5, 0, 0)
	alice.SetCreativeSlot(36, servertest.ItemStack(t, "minecraft:trapped_chest", 1))
	alice.PlaceBlock(4, 63, 0, protocol.FaceTop)
	alice.ExpectBlockAck()
	chestPropsOf(t, srv, 4, 64, 0, "minecraft:trapped_chest")
	wire, _ := registry.FindState("minecraft:redstone_wire", nil)
	if !srv.SetBlock(3, 64, 0, wire) {
		t.Fatal("the wire's chunk is not loaded")
	}
	bob := servertest.WindowedOf(t, "Bob", connect())

	wirePower := func(want string) {
		t.Helper()
		servertest.WaitFor(t, srv, 1200, func() bool {
			_, props := servertest.StatePropsOf(t, srv, 3, 64, 0)
			return props["power"] == want
		})
	}

	alice.UseBlock(4, 64, 0)
	alice.ExpectContainerOpenType(protocol.MenuGeneric9x3, protocol.BlockEntityTrappedChest, 4, 64, 0)
	alice.ExpectLidOf(4, 64, 0, 1, protocol.BlockTrappedChest)
	wirePower("1")
	alice.Settle()

	bob.UseBlock(4, 64, 0)
	bob.ExpectContainerOpenType(protocol.MenuGeneric9x3, protocol.BlockEntityTrappedChest, 4, 64, 0)
	wirePower("2")
	bob.Settle()

	alice.CloseWindow()
	alice.ExpectWindowZero()
	wirePower("1")
	bob.CloseWindow()
	bob.ExpectWindowZero()
	wirePower("0")
}

// TestAChestUnderAConductorStaysShut checks ChestBlock.isChestBlockedAt's
// block half: a full block above a chest refuses the window, while the click
// still counts as used.
func TestAChestUnderAConductorStaysShut(t *testing.T) {
	t.Parallel()
	srv, w := servertest.OpenWindowed(t, servertest.BlockTestConfig())

	if !srv.SetBlock(4, 64, 0, registry.BlockStates["minecraft:chest"]) {
		t.Fatal("the chest's chunk is not loaded")
	}
	if !srv.SetBlock(4, 65, 0, registry.BlockStates["minecraft:stone"]) {
		t.Fatal("the stone's chunk is not loaded")
	}

	w.UseBlock(4, 64, 0)
	if opened := servertest.ChestWindowOpens(t, w.Client); opened != 0 {
		t.Errorf("a chest with stone above opened %d windows, want none", opened)
	}
}
