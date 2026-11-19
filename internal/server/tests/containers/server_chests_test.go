package containers

// The double and the trapped chest: two chests side by side pair into one
// window of 54 over both halves, a hopper at either half sees the pair
// whole, the pair's lid and sound count across both halves, and a trapped
// chest powers redstone with the count of players looking in.

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"io"
	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/console"
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

// TestADoubleChestWindowClosesWhenItsOtherHalfChanges checks both halves:
// replacing the half the window is not anchored on with a barrel closes the
// window, as vanilla's CompoundContainer.stillValid asks each half.
func TestADoubleChestWindowClosesWhenItsOtherHalfChanges(t *testing.T) {
	t.Parallel()
	srv, w := servertest.OpenWindowed(t, servertest.BlockTestConfig())

	servertest.PlaceChestPair(t, w, "minecraft:chest", 4, 64, 0)
	w.UseBlock(4, 64, 0)
	w.ExpectContainerOpen(protocol.MenuGeneric9x6, true, 4, 64, 0)

	sticks := servertest.ItemStack(t, "minecraft:stick", 3)
	w.SetCreativeSlot(37, sticks)
	w.Click(82, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{82: {}}, sticks)

	console.New(strings.NewReader("setblock 4 64 0 minecraft:barrel\n"), io.Discard, io.Discard, srv).Serve()

	awaitWindowClosed(w, srv, 20, sticks)
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

// TestADoubleChestsLidAndSoundCountAcrossThePair checks the pair's lid as
// one: an open of either half tells both halves' lids the pair's viewers,
// and the open and close sounds play once, from between the halves.
func TestADoubleChestsLidAndSoundCountAcrossThePair(t *testing.T) {
	t.Parallel()
	_, connect := servertest.Start(t, servertest.BlockTestConfig())

	alice := servertest.WindowedOf(t, "Alice", connect())
	servertest.PlaceChestPair(t, alice, "minecraft:chest", 4, 64, 0)
	bob := servertest.WindowedOf(t, "Bob", connect())

	alice.UseBlock(4, 64, 0)
	// The open sound goes out while the window is built, before Open Screen.
	if name, x, y, z := alice.AwaitChestSound(); name != "minecraft:block.chest.open" || x != 5 || y != 64.5 || z != 0.5 {
		t.Errorf("the open played %s at (%g, %g, %g), want block.chest.open at (5, 64.5, 0.5)", name, x, y, z)
	}
	alice.ExpectContainerOpen(protocol.MenuGeneric9x6, true, 4, 64, 0)
	alice.ExpectLidOf(5, 64, 0, 1, protocol.BlockChest)
	alice.ExpectLidOf(4, 64, 0, 1, protocol.BlockChest)
	bob.ExpectLidOf(5, 64, 0, 1, protocol.BlockChest)
	bob.ExpectLidOf(4, 64, 0, 1, protocol.BlockChest)

	// Bob opens the other half: both lids count two, and no sound.
	bob.UseBlock(5, 64, 0)
	bob.ExpectContainerOpen(protocol.MenuGeneric9x6, true, 5, 64, 0)
	bob.ExpectLidOf(5, 64, 0, 2, protocol.BlockChest)
	bob.ExpectLidOf(4, 64, 0, 2, protocol.BlockChest)
	alice.ExpectLidOf(5, 64, 0, 2, protocol.BlockChest)
	alice.ExpectLidOf(4, 64, 0, 2, protocol.BlockChest)
	bob.SettleAndCollect(func(id int32, data []byte) {
		if id == protocol.IDPlaySoundEffect {
			t.Error("a sound played when the pair was opened a second time")
		}
	})

	alice.Settle()
	alice.CloseWindow()
	alice.ExpectWindowZero()
	bob.ExpectLidOf(5, 64, 0, 1, protocol.BlockChest)
	bob.ExpectLidOf(4, 64, 0, 1, protocol.BlockChest)

	bob.CloseWindow()
	bob.ExpectWindowZero()
	if name, x, y, z := bob.AwaitChestSound(); name != "minecraft:block.chest.close" || x != 5 || y != 64.5 || z != 0.5 {
		t.Errorf("the close played %s at (%g, %g, %g), want block.chest.close at (5, 64.5, 0.5)", name, x, y, z)
	}
	bob.ExpectLidOf(5, 64, 0, 0, protocol.BlockChest)
	bob.ExpectLidOf(4, 64, 0, 0, protocol.BlockChest)
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

// TestAChestUnderSoulSandOrMudStaysShut checks that the explicit
// Blocks::always conductors block a chest despite their 14/16 collision.
// vanilla: Blocks.SOUL_SAND, Blocks.MUD, ChestBlock.isChestBlockedAt
func TestAChestUnderSoulSandOrMudStaysShut(t *testing.T) {
	t.Parallel()
	srv, w := servertest.OpenWindowed(t, servertest.BlockTestConfig())

	if !srv.SetBlock(4, 64, 0, registry.BlockStates["minecraft:chest"]) {
		t.Fatal("the chest's chunk is not loaded")
	}
	for _, name := range []string{"minecraft:soul_sand", "minecraft:mud"} {
		if !srv.SetBlock(4, 65, 0, registry.BlockStates[name]) {
			t.Fatal("the block's chunk is not loaded")
		}
		w.UseBlock(4, 64, 0)
		if opened := servertest.ChestWindowOpens(t, w.Client); opened != 0 {
			t.Errorf("a chest with %s above opened %d windows, want none", name, opened)
		}
	}
}

// TestAChestUnderASittingCatStaysShut checks ChestBlock.isChestBlockedAt's
// cat half: a sitting cat on a chest refuses the window and a comparator
// reads 0; once the cat stands, the chest opens and reads its fill.
func TestAChestUnderASittingCatStaysShut(t *testing.T) {
	t.Parallel()
	srv, w := servertest.OpenWindowedOps(t, servertest.BlockTestConfig())

	servertest.Setblock(t, w.Client, 4, 64, 0, "chest")
	if !srv.SetInventoryAt(4, 64, 0, 0, servertest.ItemStack(t, "minecraft:dirt", 64)) {
		t.Fatal("the chest has no slot 0")
	}
	stone := registry.BlockStates["minecraft:stone"]
	srv.SetBlock(4, 63, 1, stone)
	srv.SetBlock(4, 63, 2, stone)
	servertest.Setblock(t, w.Client, 4, 64, 2, "redstone_wire")
	cat := srv.SpawnMob("minecraft:cat", 4.5, 65, 0.5)
	srv.SetMobNoAI(cat)
	srv.SetMobSitting(cat, true)
	comparator := func(want string) {
		srv.SetBlock(4, 64, 1, registry.BlockStates["minecraft:air"])
		srv.SetBlock(5, 64, 1, registry.BlockStates["minecraft:air"])
		servertest.Setblock(t, w.Client, 4, 64, 1, "comparator[facing=north]")
		servertest.Setblock(t, w.Client, 5, 64, 1, "stone")
		srv.TickPhysics(4)
		servertest.ExpectWirePower(t, srv, 4, 64, 2, want)
	}

	w.UseBlock(4, 64, 0)
	if opened := servertest.ChestWindowOpens(t, w.Client); opened != 0 {
		t.Errorf("a chest under a sitting cat opened %d windows, want none", opened)
	}
	comparator("0")

	srv.SetMobSitting(cat, false)
	w.UseBlock(4, 64, 0)
	if opened := servertest.ChestWindowOpens(t, w.Client); opened != 1 {
		t.Errorf("a chest under a standing cat opened %d windows, want one", opened)
	}
	comparator("1")
}

// TestADoubleChestUnderAConductorStaysShut checks the pair: stone above one
// half refuses the whole chest, so clicking the clear half opens nothing.
func TestADoubleChestUnderAConductorStaysShut(t *testing.T) {
	t.Parallel()
	srv, w := servertest.OpenWindowed(t, servertest.BlockTestConfig())

	servertest.PlaceChestPair(t, w, "minecraft:chest", 4, 64, 0)
	if !srv.SetBlock(4, 65, 0, registry.BlockStates["minecraft:stone"]) {
		t.Fatal("the stone's chunk is not loaded")
	}

	w.UseBlock(5, 64, 0)
	if opened := servertest.ChestWindowOpens(t, w.Client); opened != 0 {
		t.Errorf("a double chest with stone over one half opened %d windows, want none", opened)
	}
}

// TestAChestUnderGlassStillOpens checks that only a conductor blocks: glass
// above leaves the chest opening as usual.
func TestAChestUnderGlassStillOpens(t *testing.T) {
	t.Parallel()
	srv, w := servertest.OpenWindowed(t, servertest.BlockTestConfig())

	if !srv.SetBlock(4, 64, 0, registry.BlockStates["minecraft:chest"]) {
		t.Fatal("the chest's chunk is not loaded")
	}
	if !srv.SetBlock(4, 65, 0, registry.BlockStates["minecraft:glass"]) {
		t.Fatal("the glass's chunk is not loaded")
	}

	w.UseBlock(4, 64, 0)
	if opened := servertest.ChestWindowOpens(t, w.Client); opened != 1 {
		t.Errorf("a chest under glass opened %d windows, want one", opened)
	}
}

// TestAnEnderChestUnderAConductorStaysShut checks the ender chest's own
// block: a conductor above refuses its window as it does a chest's.
// vanilla: EnderChestBlock.isChestBlockedAt
func TestAnEnderChestUnderAConductorStaysShut(t *testing.T) {
	t.Parallel()
	srv, w := servertest.OpenWindowed(t, servertest.BlockTestConfig())

	if !srv.SetBlock(4, 64, 0, registry.BlockStates["minecraft:ender_chest"]) {
		t.Fatal("the ender chest's chunk is not loaded")
	}
	if !srv.SetBlock(4, 65, 0, registry.BlockStates["minecraft:stone"]) {
		t.Fatal("the stone's chunk is not loaded")
	}

	w.UseBlock(4, 64, 0)
	if opened := servertest.ChestWindowOpens(t, w.Client); opened != 0 {
		t.Errorf("an ender chest with stone above opened %d windows, want none", opened)
	}
}

// TestAChestsOpenSoundPrecedesItsWindow checks the open sound's place: it is
// the first packet of the open, ahead of Open Screen and the contents, as
// vanilla plays it while the chest's menu is built.
func TestAChestsOpenSoundPrecedesItsWindow(t *testing.T) {
	t.Parallel()
	srv, w := servertest.OpenWindowed(t, servertest.BlockTestConfig())

	if !srv.SetBlock(4, 64, 0, registry.BlockStates["minecraft:chest"]) {
		t.Fatal("the chest's chunk is not loaded")
	}

	w.UseBlock(4, 64, 0)
	// awaitChestSound fails on any packet but the background traffic before
	// the sound, so this pins the sound ahead of Open Screen.
	if name, _, _, _ := w.AwaitChestSound(); name != "minecraft:block.chest.open" {
		t.Errorf("the chest opened to the sound %q, want minecraft:block.chest.open", name)
	}
	w.ExpectContainerOpen(protocol.MenuGeneric9x3, true, 4, 64, 0)
}

// TestAChestUnderAnOpenShulkerBoxOpens checks isChestBlockedAt against a
// shulker box: a closed box is a conductor and blocks the chest, an open one
// is not.
// vanilla: ChestBlock.isChestBlockedAt, ShulkerBoxBlock.getShape
func TestAChestUnderAnOpenShulkerBoxOpens(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.BlockTestConfig())
	alice := servertest.WindowedOf(t, "Alice", connect())
	bob := servertest.WindowedOf(t, "Bob", connect())
	servertest.HoldTicks(t, srv)
	box, _ := registry.FindState("minecraft:shulker_box", map[string]string{"facing": "up"})
	if !srv.SetBlock(4, 64, 0, registry.BlockStates["minecraft:chest"]) || !srv.SetBlock(4, 65, 0, box) {
		t.Fatal("the chunk is not loaded")
	}

	bob.UseBlock(4, 64, 0)
	if opened := servertest.ChestWindowOpens(t, bob.Client); opened != 0 {
		t.Fatalf("a chest under a closed shulker box opened %d windows, want none", opened)
	}
	alice.UseBlock(4, 65, 0)
	alice.ExpectContainerOpenType(protocol.MenuShulkerBox, protocol.BlockEntityShulkerBox, 4, 65, 0)
	servertest.RunTicks(t, srv, 1)
	bob.UseBlock(4, 64, 0)
	if opened := servertest.ChestWindowOpens(t, bob.Client); opened != 1 {
		t.Errorf("a chest under an open shulker box opened %d windows, want 1", opened)
	}
}
