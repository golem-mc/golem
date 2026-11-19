package containers

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"runtime"
	"slices"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// The container moves: a hopper's feed and its drain, the cooldown between
// its moves, the redstone that stops it, the furnace's faces, a dropper's
// push, and the dispenser's registry of behaviors. The ticks are driven by
// hand - the main loop stopped before the first fill, so no tick of the
// server's own races the counts - and each move is checked at the tick it
// happens on, the way the furnace tests run their smelts. The flat world's
// ground stands at y=63, so the blocks the tests build sit from y=64 up.

// placeAHopperOnTheGround puts a hopper at the position given, standing on
// the ground and facing down, the way a player's placement of one makes it:
// clicked on its top, the hopper's spout opens downward.
func placeAHopperOnTheGround(t *testing.T, srv *server.Server, w *servertest.WindowedClient, x, y, z int32) {
	t.Helper()
	w.SetCreativeSlot(36, servertest.ItemStack(t, "minecraft:hopper", 1))
	w.PlaceBlock(x, y-1, z, protocol.FaceTop)
	w.ExpectBlockAck()
	if name, props := servertest.StatePropsOf(t, srv, x, y, z); name != "minecraft:hopper" || props["facing"] != "down" {
		t.Fatalf("the hopper stands as %s %v, want a hopper facing down", name, props)
	}
}

// digAChestUnder digs the ground out from under a block and places a chest
// in the hole, against the underside of what stands above it. The chest
// goes in against the hopper's own bottom face, so the player sneaks to
// build against the container instead of opening it.
func digAChestUnder(t *testing.T, srv *server.Server, w *servertest.WindowedClient, x, y, z int32) {
	t.Helper()
	w.SetCreativeSlot(36, servertest.ItemStack(t, "minecraft:stone_pickaxe", 1))
	w.BreakBlock(x, y, z)
	w.ExpectBlockAck()
	w.SetCreativeSlot(36, servertest.ItemStack(t, "minecraft:chest", 1))
	w.Sneak(true)
	w.PlaceBlock(x, y+1, z, protocol.FaceBottom)
	w.Sneak(false)
	w.SettleKeeping()
	srv.TickSurvival()
	w.ExpectBlockAck()
	if name, _ := servertest.StatePropsOf(t, srv, x, y, z); name != "minecraft:chest" {
		t.Fatalf("the chest stands as %s, want a chest under the hopper", name)
	}
}

// TestAHopperFeedsTheContainerBelowIt checks the hopper's push: one item of
// its first stack goes out of the spout into the container below, the stack
// it came from shrinking by it, and the next item waits out the eight ticks
// of cooldown the move plants.
func TestAHopperFeedsTheContainerBelowIt(t *testing.T) {
	t.Parallel()
	srv, w := servertest.OpenWindowed(t, servertest.BlockTestConfig())

	placeAHopperOnTheGround(t, srv, w, 4, 64, 0)
	digAChestUnder(t, srv, w, 4, 63, 0)
	srv.StopTick()

	// Two dirt in the hopper, and the main loop stopped so the hopper's
	// ticks can be counted.
	if !srv.SetInventoryAt(4, 64, 0, 0, servertest.ItemStack(t, "minecraft:dirt", 2)) {
		t.Fatal("the hopper has no block entity to fill")
	}

	// The first tick of the hopper's: one dirt out, one left, and the
	// eight ticks of cooldown standing.
	servertest.TickBlockEntitiesByHand(srv, 1)
	if got := srv.InventoryAt(4, 63, 0); servertest.NothingIn(got) || got[0].Count != 1 {
		t.Errorf("the chest below holds %+v, want one dirt in slot 0", got)
	}
	if got := srv.InventoryAt(4, 64, 0); servertest.NothingIn(got) || got[0].Count != 1 {
		t.Errorf("the hopper holds %+v, want one dirt left in slot 0", got)
	}
	if got := srv.HopperCooldownAt(4, 64, 0); got != 8 {
		t.Errorf("the hopper's cooldown after a move is %d, want 8", got)
	}

	// Seven ticks of quiet: the cooldown drains and the chest keeps the
	// one dirt it was given.
	servertest.TickBlockEntitiesByHand(srv, 7)
	if got := srv.InventoryAt(4, 63, 0); servertest.NothingIn(got) || got[0].Count != 1 {
		t.Errorf("the chest below holds %+v after the quiet, want still one dirt", got)
	}

	// The ninth tick: the cooldown spent, the second dirt follows.
	servertest.TickBlockEntitiesByHand(srv, 1)
	if got := srv.InventoryAt(4, 63, 0); servertest.NothingIn(got) || got[0].Count != 2 {
		t.Errorf("the chest below holds %+v, want both dirt", got)
	}
	if got := srv.InventoryAt(4, 64, 0); !servertest.NothingIn(got) {
		t.Errorf("the hopper holds %+v, want it empty", got)
	}
}

// TestAHopperFeedsWithItsWindowOpen checks the feed with the hopper's own
// window open: the chest below takes the first dirt, the window is told the
// slot it left, and the tick goes on - the viewer sync waits for the
// hopper's lock to be down, since a sync takes that same lock again.
func TestAHopperFeedsWithItsWindowOpen(t *testing.T) {
	t.Parallel()
	srv, w := servertest.OpenWindowed(t, servertest.BlockTestConfig())

	placeAHopperOnTheGround(t, srv, w, 4, 64, 0)
	digAChestUnder(t, srv, w, 4, 63, 0)

	// The window open while it feeds, with the main loop running: the
	// viewer is what a sync inside the tick's lock would have to tell.
	w.UseBlock(4, 64, 0)
	if open := w.ExpectContainerOpenType(protocol.MenuHopper, protocol.BlockEntityHopper, 4, 64, 0); len(open) != 5+36 {
		t.Errorf("the hopper window has %d slots, want its 5 and the player's 36", len(open))
	}
	// A tick that stops the loop holds the locks the cleanups wait on, so
	// the watchdog ends the binary instead of the suite hanging.
	watchdog := time.AfterFunc(20*time.Second, func() {
		panic(t.Name() + ": the tick stopped inside the hopper's turn")
	})
	defer watchdog.Stop()

	// Dirt into the window the player is looking at: the hopper's next turn
	// feeds the chest below and corrects the window.
	if !srv.SetInventoryAt(4, 64, 0, 0, servertest.ItemStack(t, "minecraft:dirt", 2)) {
		t.Fatal("the hopper has no block entity to fill")
	}
	servertest.WaitForTicks(t, srv, 20, func() bool {
		got := srv.InventoryAt(4, 63, 0)
		return !servertest.NothingIn(got) && got[0].Count == 1
	})
	if got := srv.InventoryAt(4, 64, 0); servertest.NothingIn(got) || got[0].Count != 1 {
		t.Errorf("the hopper holds %+v, want one dirt left of the two", got)
	}
	w.AwaitWindowSlot(0, servertest.ItemStack(t, "minecraft:dirt", 1))
}

// TestAHopperDrainsTheContainerAboveIt checks the hopper's pull: one item
// per cooldown, the first slot of the chest above giving one up and the
// rest of its slots waiting their turn, and the eight ticks of cooldown
// standing after each move.
func TestAHopperDrainsTheContainerAboveIt(t *testing.T) {
	t.Parallel()
	srv, w := servertest.OpenWindowed(t, servertest.BlockTestConfig())

	placeAHopperOnTheGround(t, srv, w, 4, 64, 0)
	digAChestUnder(t, srv, w, 4, 63, 0)

	// A chest against the hopper's top, the container the mouth reads.
	w.SetCreativeSlot(36, servertest.ItemStack(t, "minecraft:chest", 1))
	w.Sneak(true)
	w.PlaceBlock(4, 64, 0, protocol.FaceTop)
	w.Sneak(false)
	w.SettleKeeping()
	srv.TickSurvival()
	w.ExpectBlockAck()
	srv.StopTick()

	// Three different items above, one to a slot: however many slots the
	// chest holds, one tick of the hopper's moves one item of the first.
	if !srv.SetInventoryAt(4, 65, 0, 0, servertest.ItemStack(t, "minecraft:dirt", 1)) {
		t.Fatal("the chest above has no block entity to fill")
	}
	if !srv.SetInventoryAt(4, 65, 0, 1, servertest.ItemStack(t, "minecraft:stick", 1)) {
		t.Fatal("the chest above has no second slot to fill")
	}
	if !srv.SetInventoryAt(4, 65, 0, 2, servertest.ItemStack(t, "minecraft:cobblestone", 1)) {
		t.Fatal("the chest above has no third slot to fill")
	}

	// The first tick of the hopper's: one dirt in, the stick and the
	// cobblestone still above, and the eight ticks of cooldown standing.
	servertest.TickBlockEntitiesByHand(srv, 1)
	hopper := srv.InventoryAt(4, 64, 0)
	if servertest.NothingIn(hopper) || hopper[0].Count != 1 || hopper[0].ItemID != servertest.ItemStack(t, "minecraft:dirt", 1).ItemID || !hopper[1].Empty() {
		t.Errorf("the hopper holds %+v, want one dirt taken in and nothing beside it", hopper)
	}
	if got := srv.InventoryAt(4, 65, 0); len(got) < 3 || !got[0].Empty() || got[1].Empty() || got[2].Empty() {
		t.Errorf("the chest above holds %+v, want the dirt gone and the other two kept", got)
	}
	if got := srv.HopperCooldownAt(4, 64, 0); got != 8 {
		t.Errorf("the hopper's cooldown after the suck is %d, want 8", got)
	}

	// The seven ticks of quiet: nothing moves on.
	servertest.TickBlockEntitiesByHand(srv, 7)
	if got := srv.InventoryAt(4, 64, 0); servertest.NothingIn(got) || got[0].Count != 1 || !got[1].Empty() {
		t.Errorf("the hopper holds %+v after the quiet, want the one dirt alone", got)
	}

	// The ninth tick: the next slot's stick follows, one item of it, and
	// the dirt rides out to the chest below on the same tick.
	servertest.TickBlockEntitiesByHand(srv, 1)
	if got := srv.InventoryAt(4, 65, 0); len(got) < 3 || !got[0].Empty() || !got[1].Empty() || got[2].Empty() {
		t.Errorf("the chest above holds %+v, want the stick gone and the cobblestone kept", got)
	}
	hopper = srv.InventoryAt(4, 64, 0)
	if servertest.NothingIn(hopper) || hopper[0].Count != 1 || hopper[0].ItemID != servertest.ItemStack(t, "minecraft:stick", 1).ItemID || !hopper[1].Empty() {
		t.Errorf("the hopper holds %+v, want the one stick taken in", hopper)
	}
	if got := srv.InventoryAt(4, 63, 0); servertest.NothingIn(got) || got[0].Count != 1 {
		t.Errorf("the chest below holds %+v, want the dirt fed on", got)
	}
}

// TestAContainerAboveKeepsTheHopperOffTheItemsAboveIt checks the suck's
// second rule: a container above the mouth takes the whole turn, and the
// item entities lying in the suck box beside it stay where they lie.
func TestAContainerAboveKeepsTheHopperOffTheItemsAboveIt(t *testing.T) {
	t.Parallel()
	srv, w := servertest.OpenWindowed(t, servertest.BlockTestConfig())

	placeAHopperOnTheGround(t, srv, w, 4, 64, 0)

	// A chest against the hopper's top, the container the mouth reads.
	w.SetCreativeSlot(36, servertest.ItemStack(t, "minecraft:chest", 1))
	w.Sneak(true)
	w.PlaceBlock(4, 64, 0, protocol.FaceTop)
	w.Sneak(false)
	w.SettleKeeping()
	srv.TickSurvival()
	w.ExpectBlockAck()
	srv.StopTick()

	if !srv.SetInventoryAt(4, 65, 0, 0, servertest.ItemStack(t, "minecraft:dirt", 2)) {
		t.Fatal("the chest above has no block entity to fill")
	}
	// An item entity lying inside the chest's cell, where the suck box
	// reaches.
	if srv.SpawnItemEntity(4.5, 65.0, 0.5, "minecraft:stick", 7) == 0 {
		t.Fatal("the item entity did not spawn")
	}

	// The first tick of the hopper's: one dirt out of the chest, the item
	// entity left lying where it was.
	servertest.TickBlockEntitiesByHand(srv, 1)
	hopper := srv.InventoryAt(4, 64, 0)
	if servertest.NothingIn(hopper) || hopper[0].Count != 1 || hopper[0].ItemID != servertest.ItemStack(t, "minecraft:dirt", 1).ItemID || !hopper[1].Empty() {
		t.Errorf("the hopper holds %+v, want one dirt taken in and nothing beside it", hopper)
	}
	if items := srv.ItemsInDimension(world.DimensionName); len(items) != 1 || items[0].Count != 7 || items[0].Item != "minecraft:stick" {
		t.Errorf("the items above hold %+v, want the seven sticks lying on", items)
	}
	if got := srv.HopperCooldownAt(4, 64, 0); got != 8 {
		t.Errorf("the hopper's cooldown after the suck is %d, want 8", got)
	}
}

// TestAHopperSucksThroughNonFullBlocksButReadsContainersAboveIt checks the
// suck's block rule: the mouth reaches the items above a block whose
// collision does not fill the cell - oak stairs reach the cell's top without
// filling it, soul sand falls short, vanilla's isCollisionShapeFullBlock
// false for both - while a chest above is the container it drains ahead of
// that rule and a composter, a vanilla container this server keeps no window
// for, covers the mouth all the same.
func TestAHopperSucksThroughNonFullBlocksButReadsContainersAboveIt(t *testing.T) {
	t.Parallel()
	srv, w := servertest.OpenWindowed(t, servertest.BlockTestConfig())

	columns := []struct {
		x   int32
		top string
	}{
		{4, "minecraft:oak_stairs"},
		{6, "minecraft:chest"},
		{8, "minecraft:soul_sand"},
		{10, "minecraft:composter"},
	}
	for _, c := range columns {
		// The columns run east past the player's reach, so each is built
		// from a spot beside it.
		w.FacePlayer(float64(c.x)+0.5, 64, 1.5, 180, 0)
		placeAHopperOnTheGround(t, srv, w, c.x, 64, 0)
		w.SetCreativeSlot(36, servertest.ItemStack(t, c.top, 1))
		w.Sneak(true)
		w.PlaceBlock(c.x, 64, 0, protocol.FaceTop)
		w.Sneak(false)
		w.SettleKeeping()
		srv.TickSurvival()
		w.ExpectBlockAck()
	}
	srv.StopTick()

	for _, c := range columns {
		if name, _ := servertest.StatePropsOf(t, srv, c.x, 65, 0); name != c.top {
			t.Fatalf("the block above the hopper at x=%d is %s, want %s", c.x, name, c.top)
		}
	}

	// Sticks under the stairs and the soul sand, in the first and third
	// hoppers' suck boxes, and under the composter, where the mouth must
	// leave them.
	if srv.SpawnItemEntity(4.5, 65.0, 0.5, "minecraft:stick", 3) == 0 {
		t.Fatal("the item entity did not spawn")
	}
	if srv.SpawnItemEntity(8.5, 65.0, 0.5, "minecraft:stick", 3) == 0 {
		t.Fatal("the item entity did not spawn")
	}
	if srv.SpawnItemEntity(10.5, 65.0, 0.5, "minecraft:stick", 3) == 0 {
		t.Fatal("the item entity did not spawn")
	}
	// Dirt in the chest above the second hopper, the container it reads.
	if !srv.SetInventoryAt(6, 65, 0, 0, servertest.ItemStack(t, "minecraft:dirt", 2)) {
		t.Fatal("the chest above has no block entity to fill")
	}

	servertest.TickBlockEntitiesByHand(srv, 1)

	for _, x := range []int32{4, 8} {
		hopper := srv.InventoryAt(x, 64, 0)
		if servertest.NothingIn(hopper) || hopper[0].Count != 3 || hopper[0].ItemID != servertest.ItemStack(t, "minecraft:stick", 1).ItemID {
			t.Errorf("the hopper at x=%d holds %+v, want the three sticks taken through", x, hopper)
		}
	}
	if got := srv.InventoryAt(10, 64, 0); !servertest.NothingIn(got) {
		t.Errorf("the hopper under the composter holds %+v, want nothing taken", got)
	}
	hopper := srv.InventoryAt(6, 64, 0)
	if servertest.NothingIn(hopper) || hopper[0].Count != 1 || hopper[0].ItemID != servertest.ItemStack(t, "minecraft:dirt", 1).ItemID {
		t.Errorf("the hopper under the chest holds %+v, want one dirt pulled from it", hopper)
	}
	if items := srv.ItemsInDimension(world.DimensionName); len(items) != 1 || items[0].Count != 3 || items[0].Item != "minecraft:stick" {
		t.Errorf("the items above hold %+v, want the three sticks under the composter left", items)
	}
}

// TestAHopperDoesNotSuckThroughAContainerBlockWithoutAWindow checks the rest
// of the vanilla containers the window table leaves out: the decorated pot,
// the shelves and the copper chests are Containers whose collision does not
// fill the cell, so the mouth must read them as containers and leave the
// entities above them where they lie.
func TestAHopperDoesNotSuckThroughAContainerBlockWithoutAWindow(t *testing.T) {
	t.Parallel()
	srv, _ := servertest.OpenWindowed(t, servertest.BlockTestConfig())

	blocks := []struct {
		x     int32
		block string
	}{
		{4, "minecraft:decorated_pot"},
		{6, "minecraft:oak_shelf"},
		{8, "minecraft:copper_chest"},
	}
	for _, c := range blocks {
		if !srv.PlaceContainer(c.x, 64, 0, "minecraft:hopper") {
			t.Fatalf("could not place the hopper at x=%d", c.x)
		}
		state, ok := registry.FindState(c.block, nil)
		if !ok {
			t.Fatalf("the registry holds no %s", c.block)
		}
		srv.SetLoadedBlock(c.x, 65, 0, state)
		if name, _ := servertest.StatePropsOf(t, srv, c.x, 65, 0); name != c.block {
			t.Fatalf("the block above the hopper at x=%d is %s, want %s", c.x, name, c.block)
		}
	}
	srv.StopTick()

	for _, c := range blocks {
		if srv.SpawnItemEntity(float64(c.x)+0.5, 65.0, 0.5, "minecraft:stick", 3) == 0 {
			t.Fatal("the item entity did not spawn")
		}
	}

	servertest.TickBlockEntitiesByHand(srv, 1)

	for _, c := range blocks {
		if got := srv.InventoryAt(c.x, 64, 0); !servertest.NothingIn(got) {
			t.Errorf("the hopper under the %s holds %+v, want nothing taken", c.block, got)
		}
	}
	if items := srv.ItemsInDimension(world.DimensionName); len(items) != 3 {
		t.Errorf("the items above hold %+v, want all three sticks left", items)
	}
}

// TestAPartlyTakenItemEntityIsNoMove checks the suck's third rule: an item
// entity only part of whose stack fits is no move, vanilla's addItem
// answering false for a remainder, so the hopper's slots take their share
// and the cooldown is left where it stood.
func TestAPartlyTakenItemEntityIsNoMove(t *testing.T) {
	t.Parallel()
	srv, w := servertest.OpenWindowed(t, servertest.BlockTestConfig())

	placeAHopperOnTheGround(t, srv, w, 4, 64, 0)
	srv.StopTick()

	// Five near-full slots of dirt: room for five more, and no empty slot
	// for the entity's stack to fall whole into.
	for slot := range 5 {
		if !srv.SetInventoryAt(4, 64, 0, slot, servertest.ItemStack(t, "minecraft:dirt", 63)) {
			t.Fatal("the hopper has no slot to fill")
		}
	}
	// Sixty-five dirt lying above: five fit, sixty stay with the entity.
	if srv.SpawnItemEntity(4.5, 65.0, 0.5, "minecraft:dirt", 65) == 0 {
		t.Fatal("the item entity did not spawn")
	}

	servertest.TickBlockEntitiesByHand(srv, 1)
	for slot, stack := range srv.InventoryAt(4, 64, 0) {
		if stack.Count != 64 {
			t.Errorf("the hopper's slot %d holds %+v, want the one item of the share taken", slot, stack)
		}
	}
	if items := srv.ItemsInDimension(world.DimensionName); len(items) != 1 || items[0].Count != 60 {
		t.Errorf("the items above hold %+v, want the sixty that did not fit kept", items)
	}
	if got := srv.HopperCooldownAt(4, 64, 0); got != 0 {
		t.Errorf("the hopper's cooldown after the partly taken entity is %d, want none planted", got)
	}
}

// TestAnItemTakesSevenTicksPerHopperInAChain checks the cooldown a hopper
// receiving from another hopper is given: vanilla's tryMoveInItem sets
// 8 - k, k being 1 when the receiver has already ticked this game tick, so
// the item moves on seven ticks after it arrived whichever of the two ticks
// first. Two chains of three, one ticking source first and one receiver
// first, the block entities ticking in ascending x.
func TestAnItemTakesSevenTicksPerHopperInAChain(t *testing.T) {
	t.Parallel()
	srv, w := servertest.OpenWindowed(t, servertest.BlockTestConfig())

	// Each chain's last hopper faces down into the ground; the others are
	// placed against the side of the one before, facing into it.
	chains := []struct {
		name string
		xs   [3]int32 // first, middle, last
		z    int32
		face int32
	}{
		{"source first", [3]int32{4, 5, 6}, 0, protocol.FaceWest},
		{"receiver first", [3]int32{6, 5, 4}, 3, protocol.FaceEast},
	}
	for _, c := range chains {
		placeAHopperOnTheGround(t, srv, w, c.xs[2], 64, c.z)
		for i := 1; i >= 0; i-- {
			w.SetCreativeSlot(36, servertest.ItemStack(t, "minecraft:hopper", 1))
			w.Sneak(true)
			w.PlaceBlock(c.xs[i+1], 64, c.z, c.face)
			w.Sneak(false)
			w.SettleKeeping()
			srv.FlushBlockAcks()
			w.ExpectBlockAck()
		}
	}
	srv.StopTick()
	for _, c := range chains {
		if !srv.SetInventoryAt(c.xs[0], 64, c.z, 0, servertest.ItemStack(t, "minecraft:dirt", 1)) {
			t.Fatalf("%s: the first hopper has no block entity to fill", c.name)
		}
	}

	// One game tick: the game time advances, then the block entities run.
	tick := func() {
		srv.TickPhysics(1)
		srv.TickBlockEntities()
	}
	holds := func(x, z int32) bool { return !servertest.NothingIn(srv.InventoryAt(x, 64, z)) }

	// The first hopper pushes on its first tick; the middle one holds the
	// item for seven ticks and passes it on on the eighth.
	for n := 1; n <= 8; n++ {
		tick()
		for _, c := range chains {
			inMiddle := n < 8
			if got := holds(c.xs[1], c.z); got != inMiddle {
				t.Errorf("%s: after %d ticks the middle hopper holds the item = %v, want %v", c.name, n, got, inMiddle)
			}
			if got := holds(c.xs[2], c.z); got == inMiddle {
				t.Errorf("%s: after %d ticks the last hopper holds the item = %v, want %v", c.name, n, got, !inMiddle)
			}
		}
	}
}

// TestAHopperChainRunsAtVanillasRate checks a chest -> hopper -> hopper ->
// chest chain against vanilla's pushItemsTick. Vanilla ticks block entities in
// the order their tickers were registered, and a player builds the chain from
// the source down, so the top hopper's turn comes first: it ejects into the
// lower hopper before that one's own turn, and the lower hopper takes the
// chained cooldown 8 - k (seven here, its turn still ahead) rather than pulling
// a second item in the same tick. The top hopper therefore gives up one item
// per move, not two, and the counts follow vanilla's.
func TestAHopperChainRunsAtVanillasRate(t *testing.T) {
	t.Parallel()
	srv, _ := servertest.OpenWindowed(t, servertest.BlockTestConfig())
	srv.StopTick()

	// Built top down, the way the source chest and the hoppers below it go.
	for _, c := range []struct {
		y    int32
		name string
	}{
		{66, "minecraft:chest"},
		{65, "minecraft:hopper"},
		{64, "minecraft:hopper"},
		{63, "minecraft:chest"},
	} {
		if !srv.PlaceContainer(4, c.y, 0, c.name) {
			t.Fatalf("could not place %s at y=%d", c.name, c.y)
		}
	}
	if !srv.SetInventoryAt(4, 66, 0, 0, servertest.ItemStack(t, "minecraft:dirt", 64)) {
		t.Fatal("the chest above has no block entity to fill")
	}
	if !srv.SetInventoryAt(4, 65, 0, 0, servertest.ItemStack(t, "minecraft:dirt", 10)) {
		t.Fatal("the top hopper has no block entity to fill")
	}

	count := func(y int32) int {
		n := 0
		for _, s := range srv.InventoryAt(4, y, 0) {
			n += int(s.Count)
		}
		return n
	}
	for tick := 1; tick <= 40; tick++ {
		srv.TickPhysics(1)
		srv.TickBlockEntities()

		// The top hopper moves on the tick the game time is a multiple of
		// eight, the lower one the tick after, so each gives up an item every
		// eighth tick and the top hopper's count falls by one, never two.
		if want := 10 - tick/8; count(65) != want {
			t.Fatalf("after %d ticks the top hopper holds %d, want %d", tick, count(65), want)
		}
		if want := 1 + (tick-1)/8; count(64) != want {
			t.Fatalf("after %d ticks the lower hopper holds %d, want %d", tick, count(64), want)
		}
		if want := tick / 8; count(63) != want {
			t.Fatalf("after %d ticks the chest below holds %d, want %d", tick, count(63), want)
		}
		if want := 64 - (tick+7)/8; count(66) != want {
			t.Fatalf("after %d ticks the chest above holds %d, want %d", tick, count(66), want)
		}
		if tick == 1 {
			// The lower hopper's turn is still ahead when the top hopper fills
			// it, so it takes the chained cooldown 8 - k and its own turn in
			// the same game tick leaves it seven: it moves on seven ticks
			// later, not eight.
			if got := srv.HopperCooldownAt(4, 64, 0); got != 7 {
				t.Fatalf("the lower hopper's chained cooldown is %d, want 7", got)
			}
			if got := srv.HopperCooldownAt(4, 65, 0); got != 8 {
				t.Fatalf("the top hopper's cooldown after its move is %d, want 8", got)
			}
		}
	}
}

// TestAHopperStopsWhenPowered checks the redstone lock: a hopper with a
// signal beside it moves nothing, and the signal gone, its next tick moves
// again.
func TestAHopperStopsWhenPowered(t *testing.T) {
	t.Parallel()
	srv, w := servertest.OpenWindowed(t, servertest.BlockTestConfig())

	placeAHopperOnTheGround(t, srv, w, 4, 64, 0)
	digAChestUnder(t, srv, w, 4, 63, 0)

	// A redstone block against the hopper's side: the signal that stops
	// it, its own placement the change that tells the hopper.
	w.SetCreativeSlot(36, servertest.ItemStack(t, "minecraft:redstone_block", 1))
	w.Sneak(true)
	w.PlaceBlock(4, 64, 0, protocol.FaceEast)
	w.Sneak(false)
	w.SettleKeeping()
	srv.TickSurvival()
	w.ExpectBlockAck()
	srv.StopTick()

	// The neighbour change the placement booked runs a tick or two into
	// the physics, and the hopper's state carries the lock it wrote.
	srv.TickPhysics(3)
	if !srv.SetInventoryAt(4, 64, 0, 0, servertest.ItemStack(t, "minecraft:dirt", 1)) {
		t.Fatal("the hopper has no block entity to fill")
	}

	// Three ticks under the signal, and the dirt has gone nowhere.
	servertest.TickBlockEntitiesByHand(srv, 3)
	if got := srv.InventoryAt(4, 63, 0); !servertest.NothingIn(got) {
		t.Errorf("the powered hopper fed the chest %+v, want nothing moved", got)
	}
	if got := srv.InventoryAt(4, 64, 0); servertest.NothingIn(got) || got[0].Count != 1 {
		t.Errorf("the powered hopper holds %+v, want its dirt kept", got)
	}

	// The signal taken away: the neighbour change that frees the hopper
	// runs in the physics too, and the next tick of the hopper's moves
	// the dirt.
	w.BreakBlock(5, 64, 0)
	w.SettleKeeping()
	srv.TickSurvival()
	w.ExpectBlockAck()
	srv.TickPhysics(3)
	servertest.TickBlockEntitiesByHand(srv, 1)
	if got := srv.InventoryAt(4, 63, 0); servertest.NothingIn(got) || got[0].Count != 1 {
		t.Errorf("the freed hopper holds %+v below, want the dirt out", got)
	}
}

// TestAHopperBelowAFurnaceTakesItsOutputAndItsBuckets checks the furnace's
// faces from below: the output first, then the fuel slot's buckets alone -
// the coal stays, however long the hopper waits - and a hopper beside the
// furnace feeding the fuel slot through its side.
func TestAHopperBelowAFurnaceTakesItsOutputAndItsBuckets(t *testing.T) {
	t.Parallel()
	srv, w := servertest.OpenWindowed(t, servertest.BlockTestConfig())

	placeAHopperOnTheGround(t, srv, w, 4, 64, 0)

	// The furnace against the hopper's top, above the mouth the hopper
	// reads through its bottom face. Both placements click a container,
	// so both go in under the sneak.
	w.SetCreativeSlot(36, servertest.ItemStack(t, "minecraft:furnace", 1))
	w.Sneak(true)
	w.PlaceBlock(4, 64, 0, protocol.FaceTop)
	w.Sneak(false)
	w.SettleKeeping()
	srv.TickSurvival()
	w.ExpectBlockAck()
	w.SetCreativeSlot(36, servertest.ItemStack(t, "minecraft:hopper", 1))
	w.Sneak(true)
	w.PlaceBlock(4, 65, 0, protocol.FaceEast)
	w.Sneak(false)
	w.SettleKeeping()
	srv.TickSurvival()
	w.ExpectBlockAck()
	if name, props := servertest.StatePropsOf(t, srv, 5, 65, 0); name != "minecraft:hopper" || props["facing"] != "west" {
		t.Fatalf("the side hopper stands as %s %v, want one facing west into the furnace", name, props)
	}
	srv.StopTick()

	// The furnace's three slots: something to smelt, coal as its fuel,
	// and one ingot waiting in the output.
	if !srv.SetInventoryAt(4, 65, 0, 0, servertest.ItemStack(t, "minecraft:raw_iron", 1)) {
		t.Fatal("the furnace has no block entity to fill")
	}
	if !srv.SetInventoryAt(4, 65, 0, 1, servertest.ItemStack(t, "minecraft:coal", 1)) {
		t.Fatal("the furnace has no fuel slot to fill")
	}
	if !srv.SetInventoryAt(4, 65, 0, 2, servertest.ItemStack(t, "minecraft:iron_ingot", 1)) {
		t.Fatal("the furnace has no output slot to fill")
	}

	// The first tick of the hopper's: the ingot out of the output, the
	// coal left where it burns.
	servertest.TickBlockEntitiesByHand(srv, 1)
	ingot := servertest.ItemStack(t, "minecraft:iron_ingot", 1)
	if got := srv.InventoryAt(4, 64, 0); servertest.NothingIn(got) || got[0].ItemID != ingot.ItemID {
		t.Errorf("the hopper holds %+v, want the ingot the output gave", got)
	}
	if got := srv.InventoryAt(4, 65, 0); len(got) < 3 || !got[2].Empty() {
		t.Errorf("the furnace's output holds %+v, want it empty of its ingot", got)
	}

	// The output empty and a water bucket in the fuel slot: the bucket is
	// what the bottom face gives up next, eight ticks on.
	srv.SetInventoryAt(4, 65, 0, 1, servertest.ItemStack(t, "minecraft:water_bucket", 1))
	servertest.TickBlockEntitiesByHand(srv, 9)
	bucket := servertest.ItemStack(t, "minecraft:water_bucket", 1)
	if got := srv.InventoryAt(4, 64, 0); len(got) < 2 || got[1].ItemID != bucket.ItemID {
		t.Errorf("the hopper holds %+v, want the water bucket taken in beside the ingot", got)
	}

	// Coal in the fuel slot where the bucket was: the bottom face gives
	// up no fuel, and the hopper waits forever.
	srv.SetInventoryAt(4, 65, 0, 1, servertest.ItemStack(t, "minecraft:coal", 1))
	servertest.TickBlockEntitiesByHand(srv, 12)
	if got := srv.InventoryAt(4, 64, 0); len(got) < 3 || got[0].Count != 1 || got[1].Count != 1 || got[2].Count != 0 {
		t.Errorf("the hopper holds %+v, want the ingot and the bucket and nothing new of the coal", got)
	}
	if got := srv.InventoryAt(4, 65, 0); len(got) < 2 || got[1].Count != 1 {
		t.Errorf("the furnace's fuel slot holds %+v, want the coal kept", got)
	}

	// The side hopper feeds the fuel slot through the furnace's side: the
	// coal the side face reads goes where the fire eats.
	if !srv.SetInventoryAt(5, 65, 0, 0, servertest.ItemStack(t, "minecraft:coal", 2)) {
		t.Fatal("the side hopper has no block entity to fill")
	}
	servertest.TickBlockEntitiesByHand(srv, 10)
	if got := srv.InventoryAt(4, 65, 0); len(got) < 2 || got[1].Count != 3 {
		t.Errorf("the furnace's fuel slot holds %+v, want the coal it had plus the side hopper's two", got)
	}
}

// TestADropperFeedsTheContainerItFaces checks the dropper's push: the
// rising edge of a signal visits it, one item of the picked slot goes into
// the container its face opens onto, and the slot it came from shrinks.
func TestADropperFeedsTheContainerItFaces(t *testing.T) {
	t.Parallel()
	srv, w := servertest.OpenWindowed(t, servertest.BlockTestConfig())

	// The chest on the ground, and the player south of it looking north:
	// the dropper placed against the chest's north face stands at
	// (4, 64, -1) facing south, its spout into the chest.
	w.FacePlayer(4.5, 64, 1.5, 180, 0)
	w.SetCreativeSlot(36, servertest.ItemStack(t, "minecraft:chest", 1))
	w.PlaceBlock(4, 63, 0, protocol.FaceTop)
	w.ExpectBlockAck()
	w.SetCreativeSlot(36, servertest.ItemStack(t, "minecraft:dropper", 1))
	w.Sneak(true)
	w.PlaceBlock(4, 64, 0, protocol.FaceNorth)
	w.Sneak(false)
	w.SettleKeeping()
	srv.TickSurvival()
	w.ExpectBlockAck()
	if name, props := servertest.StatePropsOf(t, srv, 4, 64, -1); name != "minecraft:dropper" || props["facing"] != "south" {
		t.Fatalf("the dropper stands as %s %v, want one facing south into the chest", name, props)
	}

	// Three dirt, and the redstone block on the dropper's roof - the one
	// neighbour a dispenser block's own rules read - as the rising edge.
	srv.StopTick()
	if !srv.SetInventoryAt(4, 64, -1, 0, servertest.ItemStack(t, "minecraft:dirt", 3)) {
		t.Fatal("the dropper has no block entity to fill")
	}
	w.SetCreativeSlot(36, servertest.ItemStack(t, "minecraft:redstone_block", 1))
	w.Sneak(true)
	w.PlaceBlock(4, 64, -1, protocol.FaceTop)
	w.Sneak(false)
	w.SettleKeeping()
	srv.TickSurvival()
	w.ExpectBlockAck()

	// The visit comes four ticks after the edge books it, and the push
	// goes with the block changes it rides among.
	srv.TickPhysics(8)
	if got := srv.InventoryAt(4, 64, 0); servertest.NothingIn(got) || got[0].Count != 1 {
		t.Errorf("the chest holds %+v, want the dirt the dropper pushed", got)
	}
	if got := srv.InventoryAt(4, 64, -1); servertest.NothingIn(got) || got[0].Count != 2 {
		t.Errorf("the dropper holds %+v, want one dirt spent", got)
	}
}

// TestADispenserDropsWhatNoBehaviorClaims checks the default drop: the
// rising edge visits the dispenser, one item is born a little way out of
// the face and carried south by its birth velocity, and the slot it came
// from is spent.
func TestADispenserDropsWhatNoBehaviorClaims(t *testing.T) {
	t.Parallel()
	srv, w := servertest.OpenWindowed(t, servertest.BlockTestConfig())

	// The player looking north as they place: the dispenser stands facing
	// south, back toward them.
	w.FacePlayer(2.5, 64, 0.5, 180, 0)
	w.SetCreativeSlot(36, servertest.ItemStack(t, "minecraft:dispenser", 1))
	w.PlaceBlock(4, 63, 0, protocol.FaceTop)
	w.ExpectBlockAck()
	if name, props := servertest.StatePropsOf(t, srv, 4, 64, 0); name != "minecraft:dispenser" || props["facing"] != "south" {
		t.Fatalf("the dispenser stands as %s %v, want one facing south", name, props)
	}

	srv.StopTick()
	if !srv.SetInventoryAt(4, 64, 0, 0, servertest.ItemStack(t, "minecraft:dirt", 1)) {
		t.Fatal("the dispenser has no block entity to fill")
	}
	w.SetCreativeSlot(36, servertest.ItemStack(t, "minecraft:redstone_block", 1))
	w.Sneak(true)
	w.PlaceBlock(4, 64, 0, protocol.FaceTop)
	w.Sneak(false)
	w.SettleKeeping()
	srv.TickSurvival()
	w.ExpectBlockAck()

	srv.TickPhysics(8)
	srv.TickEntities(8)
	items := srv.ItemsInDimension(world.DimensionName)
	if len(items) != 1 {
		t.Fatalf("the dispenser left %d items, want the one it dropped", len(items))
	}
	if items[0].Item != "minecraft:dirt" || items[0].Count != 1 {
		t.Errorf("the drop carries %+v, want one dirt", items[0])
	}
	// South of the face it was born out of, on the ground it fell to, in
	// the column the face points along.
	if items[0].X < 3.6 || items[0].X > 5.4 || items[0].Y < 63.9 || items[0].Y > 65.0 || items[0].Z <= 1.4 {
		t.Errorf("the drop stands at (%.3f, %.3f, %.3f), want it carried south of the face", items[0].X, items[0].Y, items[0].Z)
	}
	if got := srv.InventoryAt(4, 64, 0); !servertest.NothingIn(got) {
		t.Errorf("the dispenser holds %+v, want the slot spent", got)
	}
}

// TestADispenserShootsAnArrow checks the arrow behavior: the rising edge
// births an arrow a little way out of the face and a tenth above the line,
// and the flight carries it south the way the face points.
func TestADispenserShootsAnArrow(t *testing.T) {
	t.Parallel()
	srv, w := servertest.OpenWindowed(t, servertest.BlockTestConfig())

	w.FacePlayer(2.5, 64, 0.5, 180, 0)
	w.SetCreativeSlot(36, servertest.ItemStack(t, "minecraft:dispenser", 1))
	w.PlaceBlock(4, 63, 0, protocol.FaceTop)
	w.ExpectBlockAck()

	srv.StopTick()
	if !srv.SetInventoryAt(4, 64, 0, 0, servertest.ItemStack(t, "minecraft:arrow", 3)) {
		t.Fatal("the dispenser has no block entity to fill")
	}
	w.SetCreativeSlot(36, servertest.ItemStack(t, "minecraft:redstone_block", 1))
	w.Sneak(true)
	w.PlaceBlock(4, 64, 0, protocol.FaceTop)
	w.Sneak(false)
	w.SettleKeeping()
	srv.TickSurvival()
	w.ExpectBlockAck()

	srv.TickPhysics(8)
	srv.TickEntities(8)
	arrows := srv.ArrowsInDimension(world.DimensionName)
	if len(arrows) != 1 {
		t.Fatalf("the dispenser left %d arrows, want the one it shot", len(arrows))
	}
	// Flown south of the face that birthed it, the way the face points.
	if arrows[0].Z <= 2.0 {
		t.Errorf("the arrow stands at (%.3f, %.3f, %.3f), want it flown south of the face", arrows[0].X, arrows[0].Y, arrows[0].Z)
	}
	if got := srv.InventoryAt(4, 64, 0); servertest.NothingIn(got) || got[0].Count != 2 {
		t.Errorf("the dispenser holds %+v, want one arrow spent of three", got)
	}
}

// TestADispenserPoursAndScoopsItsBuckets checks the two fluid behaviors:
// the filled bucket's pour where the face opens onto something replaceable,
// the empty bucket's scoop of the source it opens onto, and the bucket the
// slot is left holding either way. The player stands north-west of it all,
// out of the water's way: the pour's spread runs south, and a body the
// water reaches is a body the placement checks refuse for standing in it.
func TestADispenserPoursAndScoopsItsBuckets(t *testing.T) {
	t.Parallel()
	srv, w := servertest.OpenWindowed(t, servertest.BlockTestConfig())

	w.FacePlayer(2.5, 64, -1.5, 180, 0)
	w.SetCreativeSlot(36, servertest.ItemStack(t, "minecraft:dispenser", 1))
	w.PlaceBlock(4, 63, 0, protocol.FaceTop)
	w.ExpectBlockAck()

	srv.StopTick()
	// A water bucket in the slot, and the redstone block on the roof.
	if !srv.SetInventoryAt(4, 64, 0, 0, servertest.ItemStack(t, "minecraft:water_bucket", 1)) {
		t.Fatal("the dispenser has no block entity to fill")
	}
	w.SetCreativeSlot(36, servertest.ItemStack(t, "minecraft:redstone_block", 1))
	w.Sneak(true)
	w.PlaceBlock(4, 64, 0, protocol.FaceTop)
	w.Sneak(false)
	w.SettleKeeping()
	srv.TickSurvival()
	w.ExpectBlockAck()

	// The pour: the water stands where the face opens onto the air south
	// of the dispenser, and the slot holds the bucket it became.
	srv.TickPhysics(8)
	if name, props := servertest.StatePropsOf(t, srv, 4, 64, 1); name != "minecraft:water" || props["level"] != "0" {
		t.Errorf("the pour left %s %v at the target, want a water source", name, props)
	}
	if got := srv.InventoryAt(4, 64, 0); servertest.NothingIn(got) || got[0].ItemID != servertest.ItemStack(t, "minecraft:bucket", 1).ItemID {
		t.Errorf("the dispenser holds %+v, want the bucket the pour left", got)
	}

	// The scoop: the bucket back in the slot, the water gone, and the
	// trigger that visits it again being the redstone block lifted and
	// set back down - the fall of the edge first, the rise after.
	w.BreakBlock(4, 65, 0)
	w.SettleKeeping()
	srv.TickSurvival()
	w.ExpectBlockAck()
	srv.TickPhysics(8)
	srv.SetInventoryAt(4, 64, 0, 0, servertest.ItemStack(t, "minecraft:bucket", 1))
	w.SetCreativeSlot(36, servertest.ItemStack(t, "minecraft:redstone_block", 1))
	w.Sneak(true)
	w.PlaceBlock(4, 64, 0, protocol.FaceTop)
	w.Sneak(false)
	w.SettleKeeping()
	srv.TickSurvival()
	w.ExpectBlockAck()
	srv.TickPhysics(8)
	if name, _ := servertest.StatePropsOf(t, srv, 4, 64, 1); name != "minecraft:air" {
		t.Errorf("the scoop left %s at the target, want the water taken", name)
	}
	if got := srv.InventoryAt(4, 64, 0); servertest.NothingIn(got) || got[0].ItemID != servertest.ItemStack(t, "minecraft:water_bucket", 1).ItemID {
		t.Errorf("the dispenser holds %+v, want the water bucket the scoop filled", got)
	}
}

// TestADispenserPoursWhileAComparatorReadsIt races the pour against the
// comparator's read of the dispenser: a connection flipping the comparator
// holds the engine's lock while its recompute reads the dispenser's analog
// output, which waits for the dispenser's lock, so a visit that poured
// holding that lock would carry it all the way into the engine's - the two
// waiting on each other for good. The visit asks the world only once its
// lock is down, and both sides finish.
func TestADispenserPoursWhileAComparatorReadsIt(t *testing.T) {
	t.Parallel()
	srv, w := servertest.OpenWindowed(t, servertest.BlockTestConfig())

	// The dispenser facing south, and the comparator north of it facing
	// south, its front input the dispenser.
	w.FacePlayer(2.5, 64, 0.5, 180, 0)
	w.SetCreativeSlot(36, servertest.ItemStack(t, "minecraft:dispenser", 1))
	w.PlaceBlock(4, 63, 0, protocol.FaceTop)
	w.ExpectBlockAck()
	if name, props := servertest.StatePropsOf(t, srv, 4, 64, 0); name != "minecraft:dispenser" || props["facing"] != "south" {
		t.Fatalf("the dispenser stands as %s %v, want one facing south", name, props)
	}
	w.SetCreativeSlot(36, servertest.ItemStack(t, "minecraft:comparator", 1))
	w.Sneak(true)
	w.PlaceBlock(4, 64, 0, protocol.FaceNorth)
	w.Sneak(false)
	w.ExpectBlockAck()
	if name, props := servertest.StatePropsOf(t, srv, 4, 64, -1); name != "minecraft:comparator" || props["facing"] != "south" {
		t.Fatalf("the comparator stands as %s %v, want one facing south into the dispenser", name, props)
	}
	srv.StopTick()

	// The watchdog ends the binary with the goroutines' stacks when the
	// two sides stop answering each other: a failing t.Fatal would run the
	// cleanups against the very locks the deadlocked pair holds. The drain
	// waits past it, so a hang is this panic with the stacks rather than a
	// ping timeout on the busy cores.
	const watchdogAfter = 20 * time.Second
	watchdog := time.AfterFunc(watchdogAfter, func() {
		buf := make([]byte, 1<<22)
		n := runtime.Stack(buf, true)
		panic(t.Name() + ": the pour and the comparator read deadlocked\n" + string(buf[:n]))
	})
	defer watchdog.Stop()

	// A moment and a half of both sides, the pour driven by hand and the
	// read the connection's own handling: a pour that ran under the
	// dispenser's lock and a read held under the engine's deadlock the
	// pair within the window.
	const racedFor = 1500 * time.Millisecond
	deadline := time.Now().Add(racedFor)
	var races sync.WaitGroup
	races.Add(2)
	go func() {
		defer races.Done()
		for time.Now().Before(deadline) {
			// The cell the face opens onto air again, and the slot a
			// water bucket: a pour that can succeed, the visit whose
			// block change crosses to the engine.
			srv.SetLoadedBlock(4, 64, 1, 0)
			srv.SetInventoryAt(4, 64, 0, 0, servertest.ItemStack(t, "minecraft:water_bucket", 1))
			srv.Dispense(4, 64, 0, protocol.FaceSouth)
		}
	}()
	go func() {
		defer races.Done()
		for read := 0; time.Now().Before(deadline); read++ {
			// The comparator's click: the engine's lock held through the
			// recompute that reads the dispenser's analog output.
			w.UseBlock(4, 64, -1)
			if read%64 == 63 {
				// The answers read off the line keep it flowing, the
				// keep-alives among them answered on the way. The wait
				// runs past the watchdog: a slow core answers late, a
				// deadlock never does and the watchdog's panic speaks.
				w.DrainWithin(watchdogAfter + time.Minute)
			}
		}
	}()
	races.Wait()
	watchdog.Stop()

	// A pour that survives the race still pours: the water stands where
	// the face opened, and the slot holds the bucket it became.
	srv.SetLoadedBlock(4, 64, 1, 0)
	srv.SetInventoryAt(4, 64, 0, 0, servertest.ItemStack(t, "minecraft:water_bucket", 1))
	srv.Dispense(4, 64, 0, protocol.FaceSouth)
	if name, props := servertest.StatePropsOf(t, srv, 4, 64, 1); name != "minecraft:water" || props["level"] != "0" {
		t.Errorf("the pour left %s %v at the target, want a water source", name, props)
	}
	if got := srv.InventoryAt(4, 64, 0); servertest.NothingIn(got) || got[0].ItemID != servertest.ItemStack(t, "minecraft:bucket", 1).ItemID {
		t.Errorf("the dispenser holds %+v, want the bucket the pour left", got)
	}
}

// TestAPourRacingAContainerClickSpendsItsBucket races the pour against a
// container click into the window the act runs with the dispenser's lock
// down: the click moves what slot 0 then holds to slot 1, the way a player
// dragging the stack out mid-act would. The item left the slot with the
// decision and the commit gives back only what the act returns, so the pour
// still spends one of its buckets and leaves the emptied one.
func TestAPourRacingAContainerClickSpendsItsBucket(t *testing.T) {
	t.Parallel()
	srv, w := servertest.OpenWindowed(t, servertest.BlockTestConfig())

	// The dispenser facing south, two water buckets so the click has a
	// stack to move whichever side of the decision it lands on.
	w.FacePlayer(2.5, 64, -1.5, 180, 0)
	w.SetCreativeSlot(36, servertest.ItemStack(t, "minecraft:dispenser", 1))
	w.PlaceBlock(4, 63, 0, protocol.FaceTop)
	w.ExpectBlockAck()
	srv.StopTick()
	srv.SetInventoryAt(4, 64, 0, 0, servertest.ItemStack(t, "minecraft:water_bucket", 2))

	srv.SetDispenseActHook(func() {
		moving := srv.InventoryAt(4, 64, 0)[0]
		srv.SetInventoryAt(4, 64, 0, 0, protocol.ItemStack{})
		srv.SetInventoryAt(4, 64, 0, 1, moving)
	})
	defer srv.SetDispenseActHook(nil)

	srv.Dispense(4, 64, 0, protocol.FaceSouth)

	if name, props := servertest.StatePropsOf(t, srv, 4, 64, 1); name != "minecraft:water" || props["level"] != "0" {
		t.Fatalf("the pour left %s %v at the target, want a water source", name, props)
	}
	held := srv.InventoryAt(4, 64, 0)
	count := func(stack protocol.ItemStack) (n int32) {
		for _, got := range held {
			if got.ItemID == stack.ItemID {
				n += got.Count
			}
		}
		return n
	}
	if n := count(servertest.ItemStack(t, "minecraft:water_bucket", 1)); n != 1 {
		t.Errorf("the dispenser holds %d water buckets after the pour, want the one the click moved aside: %+v", n, held)
	}
	if n := count(servertest.ItemStack(t, "minecraft:bucket", 1)); n != 1 {
		t.Errorf("the dispenser holds %d empty buckets after the pour, want the one the pour left: %+v", n, held)
	}
}

// TestABreakDuringThePourDropsWhatTheActOwes breaks the dispenser in the
// window the act runs with its lock down: the break removes the block
// entity and spills the slots the taken item has already left, so the
// bucket the pour left has no slot to wait the commit out in. The commit
// refuses an entity no longer the world's, and the bucket is dropped where
// the dispenser stood.
func TestABreakDuringThePourDropsWhatTheActOwes(t *testing.T) {
	t.Parallel()
	srv, w := servertest.OpenWindowed(t, servertest.BlockTestConfig())

	// The dispenser facing south, one water bucket to pour.
	w.FacePlayer(2.5, 64, -1.5, 180, 0)
	w.SetCreativeSlot(36, servertest.ItemStack(t, "minecraft:dispenser", 1))
	w.PlaceBlock(4, 63, 0, protocol.FaceTop)
	w.ExpectBlockAck()
	srv.StopTick()
	srv.SetInventoryAt(4, 64, 0, 0, servertest.ItemStack(t, "minecraft:water_bucket", 1))

	srv.SetDispenseActHook(func() {
		w.BreakBlock(4, 64, 0)
		servertest.WaitFor(t, srv, 100, func() bool { return srv.InventoryAt(4, 64, 0) == nil })
	})
	defer srv.SetDispenseActHook(nil)

	srv.Dispense(4, 64, 0, protocol.FaceSouth)

	if name, props := servertest.StatePropsOf(t, srv, 4, 64, 1); name != "minecraft:water" || props["level"] != "0" {
		t.Errorf("the pour left %s %v at the target, want a water source", name, props)
	}
	var buckets int32
	for _, it := range srv.ItemsInDimension(world.DimensionName) {
		if it.Item == "minecraft:bucket" {
			buckets += it.Count
		}
	}
	if buckets != 1 {
		t.Errorf("the ground holds %d empty buckets after the pour, want the one the broken dispenser owed", buckets)
	}
}

// TestAnUnloadDuringTheActDropsWhatTheActOwes releases the dispenser's
// chunk in the window the act runs with its lock down: the leaving player's
// release saves the slots the taken item has already left and drops the
// entity, so the bone meal a face with nothing to grow keeps has no slot to
// wait the commit out in. The commit refuses the dropped entity, and the
// bone meal is dropped where the dispenser stood.
func TestAnUnloadDuringTheActDropsWhatTheActOwes(t *testing.T) {
	t.Parallel()
	srv, w := servertest.OpenWindowed(t, servertest.BlockTestConfig())

	// The dispenser facing south, one bone meal, its face opening onto
	// nothing that grows: the visit keeps the bone meal and the commit
	// owes it the slot.
	w.FacePlayer(2.5, 64, -1.5, 180, 0)
	w.SetCreativeSlot(36, servertest.ItemStack(t, "minecraft:dispenser", 1))
	w.PlaceBlock(4, 63, 0, protocol.FaceTop)
	w.ExpectBlockAck()
	srv.StopTick()
	srv.SetInventoryAt(4, 64, 0, 0, servertest.ItemStack(t, "minecraft:bone_meal", 1))

	srv.SetDispenseActHook(func() {
		w.Disconnect()
		servertest.WaitFor(t, srv, 200, func() bool { return srv.InventoryAt(4, 64, 0) == nil })
	})
	defer srv.SetDispenseActHook(nil)

	srv.Dispense(4, 64, 0, protocol.FaceSouth)

	var meals int32
	for _, it := range srv.ItemsInDimension(world.DimensionName) {
		if it.Item == "minecraft:bone_meal" {
			meals += it.Count
		}
	}
	if meals != 1 {
		t.Errorf("the ground holds %d bone meal after the unload, want the one the visit kept", meals)
	}
}

// TestAnUnloadSaveInFlightKeepsWhatTheActOwes releases the dispenser's
// chunk mid-act — the release save, still running, having read the slot
// the taken item left — and lets the commit's give-back land in that
// window. The commit's mark under the dispenser's lock makes the release
// re-save before the chunk may leave, and a chunk that beat the commit
// makes the commit drop the stack instead: either way the bone meal the
// visit kept is somewhere to be counted, never written into a slot the
// chunk dropped.
func TestAnUnloadSaveInFlightKeepsWhatTheActOwes(t *testing.T) {
	t.Parallel()
	for _, yields := range []int{1, 20, 400} {
		srv, w := servertest.OpenWindowed(t, servertest.BlockTestConfig())

		// The dispenser facing south, one bone meal, its face opening onto
		// nothing that grows: the visit keeps it and the commit owes the
		// slot.
		w.FacePlayer(2.5, 64, -1.5, 180, 0)
		w.SetCreativeSlot(36, servertest.ItemStack(t, "minecraft:dispenser", 1))
		w.PlaceBlock(4, 63, 0, protocol.FaceTop)
		w.ExpectBlockAck()
		srv.StopTick()
		srv.SetInventoryAt(4, 64, 0, 0, servertest.ItemStack(t, "minecraft:bone_meal", 1))

		// The window's client gone, the chunk unloads with what the setup
		// left; the test then holds it alone, marked dirty, so the hook's
		// release is the leaving one whose save the visit races.
		w.Disconnect()
		servertest.WaitFor(t, srv, 200, func() bool { return srv.InventoryAt(4, 64, 0) == nil })
		srv.FlushSaves()
		release := srv.HoldChunk(4, 0)
		srv.TouchChunk(4, 0)

		srv.SetDispenseActHook(func() {
			release() // the save is submitted; the yields hold the commit back
			for range yields {
				runtime.Gosched()
			}
		})
		defer srv.SetDispenseActHook(nil)

		srv.Dispense(4, 64, 0, protocol.FaceSouth)
		srv.FlushSaves()

		// The chunk is back — from the region file, or never left — and
		// the bone meal is in it or on the ground.
		reset := srv.HoldChunk(4, 0)
		defer reset()
		var meals int32
		for _, stack := range srv.InventoryAt(4, 64, 0) {
			if stack.ItemID == servertest.ItemStack(t, "minecraft:bone_meal", 1).ItemID {
				meals += stack.Count
			}
		}
		for _, it := range srv.ItemsInDimension(world.DimensionName) {
			if it.Item == "minecraft:bone_meal" {
				meals += it.Count
			}
		}
		if meals != 1 {
			t.Errorf("after %d yields, %d bone meal accounted for, want the one kept (in the slot or on the ground)", yields, meals)
		}
	}
}

// TestAGoneDispenserPlaysNoFailEvent takes the block entity out between the
// visit's world lookup and the lock its decision runs under, the window a
// break or an unload opens. Vanilla's dispenseFrom returns silently for a
// missing block entity, where an empty dispenser clicks 1001, so a gone
// visit plays nothing while one that finds the dispenser empty still clicks.
func TestAGoneDispenserPlaysNoFailEvent(t *testing.T) {
	t.Parallel()
	srv, w := servertest.OpenWindowed(t, servertest.BlockTestConfig())

	w.FacePlayer(2.5, 64, -1.5, 180, 0)
	w.SetCreativeSlot(36, servertest.ItemStack(t, "minecraft:dispenser", 1))
	w.PlaceBlock(4, 63, 0, protocol.FaceTop)
	w.ExpectBlockAck()
	srv.StopTick()

	// Records the world events the connection hears through a settle.
	drain := func() {
		w.SettleAndCollect(func(id int32, data []byte) { w.Record(id, data) })
	}

	// The empty dispenser clicks: the event the gone visit must not play.
	srv.Dispense(4, 64, 0, protocol.FaceSouth)
	drain()
	if !slices.Contains(w.Events, protocol.WorldEventDispenseFail) {
		t.Fatalf("an empty dispenser played no fail event; events were %v", w.Events)
	}

	// One item, and the entity gone by the time the decision takes the
	// lock: the lookup found it, so the gone result is what keeps it quiet.
	w.Events = nil
	srv.SetInventoryAt(4, 64, 0, 0, servertest.ItemStack(t, "minecraft:water_bucket", 1))
	srv.SetDispenseDecideHook(func() { srv.RemoveBlockEntity(4, 64, 0) })
	defer srv.SetDispenseDecideHook(nil)

	srv.Dispense(4, 64, 0, protocol.FaceSouth)
	drain()
	if slices.Contains(w.Events, protocol.WorldEventDispenseFail) {
		t.Errorf("a gone dispenser played the fail event; events were %v", w.Events)
	}
}

// TestABreakInTheDispenseLookupWindowSpillsTheStackOnce breaks the
// dispenser between the visit's lookup of its block entity and the lock its
// decision takes, the window a creative break on the connection goroutine
// opens. The decision must find the entity gone: taking the orphan's slot
// would drop a second dirt beside the one the break already spilled.
func TestABreakInTheDispenseLookupWindowSpillsTheStackOnce(t *testing.T) {
	t.Parallel()
	srv, w := servertest.OpenWindowed(t, servertest.BlockTestConfig())

	// The dispenser facing south, one dirt to drop.
	w.FacePlayer(2.5, 64, -1.5, 180, 0)
	w.SetCreativeSlot(36, servertest.ItemStack(t, "minecraft:dispenser", 1))
	w.PlaceBlock(4, 63, 0, protocol.FaceTop)
	w.ExpectBlockAck()
	srv.StopTick()
	srv.SetInventoryAt(4, 64, 0, 0, servertest.ItemStack(t, "minecraft:dirt", 1))

	groundedDirt := func() int32 {
		var n int32
		for _, it := range srv.ItemsInDimension(world.DimensionName) {
			if it.Item == "minecraft:dirt" {
				n += it.Count
			}
		}
		return n
	}

	// The break runs on the connection goroutine while the visit waits at
	// the seam, removing the entity and spilling the slot before the
	// decision can take it.
	srv.SetDispenseDecideHook(func() {
		w.BreakBlock(4, 64, 0)
		servertest.WaitFor(t, srv, 100, func() bool {
			return srv.InventoryAt(4, 64, 0) == nil && groundedDirt() == 1
		})
	})
	defer srv.SetDispenseDecideHook(nil)

	srv.Dispense(4, 64, 0, protocol.FaceSouth)

	if n := groundedDirt(); n != 1 {
		t.Errorf("the ground holds %d dirt after the break, want the one the broken dispenser spilled", n)
	}
}

// TestADispenserLightsItsTNT checks the TNT behavior: the rising edge
// stands a primed body half a block along inside the target, burning the
// eighty ticks the constructor plants, and the slot is spent.
func TestADispenserLightsItsTNT(t *testing.T) {
	t.Parallel()
	srv, w := servertest.OpenWindowed(t, servertest.BlockTestConfig())

	w.FacePlayer(2.5, 64, 0.5, 180, 0)
	w.SetCreativeSlot(36, servertest.ItemStack(t, "minecraft:dispenser", 1))
	w.PlaceBlock(4, 63, 0, protocol.FaceTop)
	w.ExpectBlockAck()

	srv.StopTick()
	if !srv.SetInventoryAt(4, 64, 0, 0, servertest.ItemStack(t, "minecraft:tnt", 1)) {
		t.Fatal("the dispenser has no block entity to fill")
	}
	w.SetCreativeSlot(36, servertest.ItemStack(t, "minecraft:redstone_block", 1))
	w.Sneak(true)
	w.PlaceBlock(4, 64, 0, protocol.FaceTop)
	w.Sneak(false)
	w.SettleKeeping()
	srv.TickSurvival()
	w.ExpectBlockAck()

	srv.TickPhysics(8)
	srv.TickEntities(8)
	tnts := srv.TNTsInDimension(world.DimensionName)
	if len(tnts) != 1 {
		t.Fatalf("the dispenser left %d primed bodies, want the one it lit", len(tnts))
	}
	// The burn is the eighty the birth planted, run down by the ticks
	// the entities have taken since.
	if tnts[0].Fuse >= 80 || tnts[0].Fuse < 70 {
		t.Errorf("the body burns with %d fuse, want the eighty the birth planted, spent down", tnts[0].Fuse)
	}
	if tnts[0].X < 4.3 || tnts[0].X > 4.7 || tnts[0].Z < 1.3 || tnts[0].Z > 1.7 {
		t.Errorf("the body stands at (%.3f, %.3f, %.3f), want it half a block along inside the target", tnts[0].X, tnts[0].Y, tnts[0].Z)
	}
	if got := srv.InventoryAt(4, 64, 0); !servertest.NothingIn(got) {
		t.Errorf("the dispenser holds %+v, want the slot spent", got)
	}
}

// TestADispenserGrowsWithItsBoneMeal checks the bone meal behavior: the
// crop the face opens onto ripens the way a right-click's draw grows it,
// the slot is spent, and a face that opens onto nothing growing is the
// fail sound and the bone meal unspent.
func TestADispenserGrowsWithItsBoneMeal(t *testing.T) {
	t.Parallel()
	srv, w := servertest.OpenWindowed(t, servertest.BlockTestConfig())

	w.FacePlayer(2.5, 64, 0.5, 180, 0)
	w.SetCreativeSlot(36, servertest.ItemStack(t, "minecraft:dispenser", 1))
	w.PlaceBlock(4, 63, 0, protocol.FaceTop)
	w.ExpectBlockAck()

	// A young wheat where the face opens onto, planted straight into the
	// world at its age of nothing.
	young, ok := registry.FindState("minecraft:wheat", map[string]string{"age": "0"})
	if !ok {
		t.Fatal("the registry holds no wheat")
	}
	srv.SetLoadedBlock(4, 64, 1, young)

	srv.StopTick()
	if !srv.SetInventoryAt(4, 64, 0, 0, servertest.ItemStack(t, "minecraft:bone_meal", 1)) {
		t.Fatal("the dispenser has no block entity to fill")
	}
	w.SetCreativeSlot(36, servertest.ItemStack(t, "minecraft:redstone_block", 1))
	w.Sneak(true)
	w.PlaceBlock(4, 64, 0, protocol.FaceTop)
	w.Sneak(false)
	w.SettleKeeping()
	srv.TickSurvival()
	w.ExpectBlockAck()

	srv.TickPhysics(8)
	_, props := servertest.StatePropsOf(t, srv, 4, 64, 1)
	age, err := strconv.Atoi(props["age"])
	if err != nil || age == 0 {
		t.Errorf("the wheat stands at age %q, want the draw to have ripened it", props["age"])
	}
	if got := srv.InventoryAt(4, 64, 0); !servertest.NothingIn(got) {
		t.Errorf("the dispenser holds %+v, want the bone meal spent", got)
	}

	// Nothing growing in front of it now - the wheat taken away - the
	// next visit is the fail sound and the bone meal kept.
	w.BreakBlock(4, 64, 1)
	w.SettleKeeping()
	srv.TickSurvival()
	w.ExpectBlockAck()
	srv.SetInventoryAt(4, 64, 0, 0, servertest.ItemStack(t, "minecraft:bone_meal", 1))
	w.SetCreativeSlot(36, servertest.ItemStack(t, "minecraft:redstone_block", 1))
	w.Sneak(true)
	w.PlaceBlock(4, 64, 0, protocol.FaceTop)
	w.Sneak(false)
	w.SettleKeeping()
	srv.TickSurvival()
	w.ExpectBlockAck()
	srv.TickPhysics(8)
	if got := srv.InventoryAt(4, 64, 0); servertest.NothingIn(got) || got[0].Count != 1 {
		t.Errorf("the dispenser holds %+v, want the bone meal kept where nothing grew", got)
	}
}

// watchTheTick arms the watchdog a dispense visit needs: a visit that stops
// the tick holds locks the cleanups wait on, so a failing t.Fatal would hang
// and the watchdog ends the binary instead.
func watchTheTick(t *testing.T) (stop func()) {
	t.Helper()
	watchdog := time.AfterFunc(20*time.Second, func() {
		panic(t.Name() + ": the tick stopped inside the dispense visit")
	})
	return func() { watchdog.Stop() }
}

// TestADispenserFiresWithItsWindowOpen checks the dispense visit with the
// dispenser's own window open: the slot is spent, the window is told the
// empty slot, and the tick goes on - the viewer sync takes the dispenser's
// lock only once the visit is out of it, since the sync would take it again.
func TestADispenserFiresWithItsWindowOpen(t *testing.T) {
	t.Parallel()
	srv, w := servertest.OpenWindowed(t, servertest.BlockTestConfig())

	w.FacePlayer(2.5, 64, 0.5, 180, 0)
	w.SetCreativeSlot(36, servertest.ItemStack(t, "minecraft:dispenser", 1))
	w.PlaceBlock(4, 63, 0, protocol.FaceTop)
	w.ExpectBlockAck()
	if name, props := servertest.StatePropsOf(t, srv, 4, 64, 0); name != "minecraft:dispenser" || props["facing"] != "south" {
		t.Fatalf("the dispenser stands as %s %v, want one facing south", name, props)
	}

	if !srv.SetInventoryAt(4, 64, 0, 0, servertest.ItemStack(t, "minecraft:dirt", 1)) {
		t.Fatal("the dispenser has no block entity to fill")
	}

	// The window open while it fires, with the main loop running.
	w.UseBlock(4, 64, 0)
	open := w.ExpectContainerOpenType(protocol.MenuGeneric3x3, protocol.BlockEntityDispenser, 4, 64, 0)
	if len(open) < 1 || !open[0].Equal(servertest.ItemStack(t, "minecraft:dirt", 1)) {
		t.Errorf("the window opened on %+v, want the dirt in slot 0", open)
	}
	defer watchTheTick(t)()

	// The redstone block on the roof is the rising edge.
	w.SetCreativeSlot(36, servertest.ItemStack(t, "minecraft:redstone_block", 1))
	w.Sneak(true)
	w.PlaceBlock(4, 64, 0, protocol.FaceTop)
	w.Sneak(false)
	w.ExpectBlockAck()

	// The visit comes a few ticks after the edge. It empties the slot before
	// it tells the window, so its tick is waited out before the correction
	// is read.
	servertest.WaitForTicks(t, srv, 20, func() bool {
		return servertest.NothingIn(srv.InventoryAt(4, 64, 0))
	})
	servertest.AwaitTicks(t, srv, 1)
	w.ExpectSlotCorrection(0, protocol.ItemStack{})
}

// TestADispenserGrowsASaplingWithItsBoneMeal checks the dispenser on a
// sapling: the sapling's own rule answers the draw, the slot is spent and
// the sparkle plays at the sapling, whether or not the dice let the stage
// move.
func TestADispenserGrowsASaplingWithItsBoneMeal(t *testing.T) {
	t.Parallel()
	srv, w := servertest.OpenWindowed(t, servertest.BlockTestConfig())

	w.FacePlayer(2.5, 64, 0.5, 180, 0)
	w.SetCreativeSlot(36, servertest.ItemStack(t, "minecraft:dispenser", 1))
	w.PlaceBlock(4, 63, 0, protocol.FaceTop)
	w.ExpectBlockAck()

	sapling, ok := registry.FindState("minecraft:oak_sapling", nil)
	if !ok {
		t.Fatal("the registry holds no oak sapling")
	}
	srv.SetLoadedBlock(4, 64, 1, sapling)

	srv.StopTick()
	if !srv.SetInventoryAt(4, 64, 0, 0, servertest.ItemStack(t, "minecraft:bone_meal", 1)) {
		t.Fatal("the dispenser has no block entity to fill")
	}
	w.SetCreativeSlot(36, servertest.ItemStack(t, "minecraft:redstone_block", 1))
	w.Sneak(true)
	w.PlaceBlock(4, 64, 0, protocol.FaceTop)
	w.Sneak(false)
	w.SettleKeeping()
	srv.FlushBlockAcks()
	w.ExpectBlockAck()

	srv.TickPhysics(8)
	if got := srv.InventoryAt(4, 64, 0); !servertest.NothingIn(got) {
		t.Errorf("the dispenser holds %+v, want the bone meal spent on the sapling", got)
	}
	sparkle := slices.Contains(w.Events, protocol.WorldEventBonemeal)
	if !sparkle {
		// The event can sit undelivered; the read waits for it, the
		// packets the dispenser also sent stepped over.
		sparkle = w.Pump(servertest.ExpectTimeout, nil, func(id int32, data []byte) bool {
			return id == protocol.IDPlayWorldEvent &&
				protocol.NewReader(data).ReadI32() == protocol.WorldEventBonemeal
		})
	}
	if !sparkle {
		t.Error("no bone meal sparkle played at the sapling")
	}
}

// TestADropperFeedsWithItsWindowOpen checks the dropper's push with the
// dropper's own window open: the chest takes the item, the window is told
// the slot it came from, and the tick goes on - the viewer syncs wait for
// the locks to be down, since a sync would take the same lock again.
func TestADropperFeedsWithItsWindowOpen(t *testing.T) {
	t.Parallel()
	srv, w := servertest.OpenWindowed(t, servertest.BlockTestConfig())

	// The chest on the ground, and the player south of it looking north:
	// the dropper placed against the chest's north face stands at
	// (4, 64, -1) facing south, its spout into the chest.
	w.FacePlayer(4.5, 64, 1.5, 180, 0)
	w.SetCreativeSlot(36, servertest.ItemStack(t, "minecraft:chest", 1))
	w.PlaceBlock(4, 63, 0, protocol.FaceTop)
	w.ExpectBlockAck()
	w.SetCreativeSlot(36, servertest.ItemStack(t, "minecraft:dropper", 1))
	w.Sneak(true)
	w.PlaceBlock(4, 64, 0, protocol.FaceNorth)
	w.Sneak(false)
	w.ExpectBlockAck()
	if name, props := servertest.StatePropsOf(t, srv, 4, 64, -1); name != "minecraft:dropper" || props["facing"] != "south" {
		t.Fatalf("the dropper stands as %s %v, want one facing south into the chest", name, props)
	}

	if !srv.SetInventoryAt(4, 64, -1, 0, servertest.ItemStack(t, "minecraft:dirt", 3)) {
		t.Fatal("the dropper has no block entity to fill")
	}

	// The dropper's own window open while it pushes.
	w.UseBlock(4, 64, -1)
	open := w.ExpectContainerOpenType(protocol.MenuGeneric3x3, protocol.BlockEntityDropper, 4, 64, -1)
	if len(open) < 1 || !open[0].Equal(servertest.ItemStack(t, "minecraft:dirt", 3)) {
		t.Errorf("the window opened on %+v, want the three dirt in slot 0", open)
	}
	defer watchTheTick(t)()

	// The redstone block on the dropper's roof is the rising edge.
	w.SetCreativeSlot(36, servertest.ItemStack(t, "minecraft:redstone_block", 1))
	w.Sneak(true)
	w.PlaceBlock(4, 64, -1, protocol.FaceTop)
	w.Sneak(false)
	w.SettleKeeping()
	srv.FlushBlockAcks()
	w.ExpectBlockAck()

	// The push lands in the chest a few ticks after the edge.
	servertest.WaitForTicks(t, srv, 20, func() bool {
		got := srv.InventoryAt(4, 64, 0)
		return !servertest.NothingIn(got) && got[0].Count == 1
	})
	if got := srv.InventoryAt(4, 64, -1); servertest.NothingIn(got) || got[0].Count != 2 {
		t.Errorf("the dropper holds %+v, want one dirt spent of three", got)
	}
	w.ExpectSlotCorrection(0, servertest.ItemStack(t, "minecraft:dirt", 2))
}

// TestAPitchAwarePlacementFacesTheLook checks the facings the placement
// rules read the look for: a barrel placed by a player looking level north
// faces south toward them, and one placed by a player looking straight down
// at the ground they stand it on faces up.
func TestAPitchAwarePlacementFacesTheLook(t *testing.T) {
	t.Parallel()
	srv, w := servertest.OpenWindowed(t, servertest.BlockTestConfig())

	// Level and north: the barrel faces south.
	w.FacePlayer(2.5, 64, 0.5, 180, 0)
	w.SetCreativeSlot(36, servertest.ItemStack(t, "minecraft:barrel", 1))
	w.PlaceBlock(4, 63, 0, protocol.FaceTop)
	w.ExpectBlockAck()
	if name, props := servertest.StatePropsOf(t, srv, 4, 64, 0); name != "minecraft:barrel" || props["facing"] != "south" {
		t.Errorf("the barrel stands as %s %v, want one facing south", name, props)
	}

	// Straight down at the ground, the player standing aside so the cell
	// they would fill holds nobody: the barrel faces up.
	w.SetCreativeSlot(36, servertest.ItemStack(t, "minecraft:barrel", 1))
	w.FacePlayer(8.5, 64, 1.5, 180, 90)
	w.PlaceBlock(8, 63, 0, protocol.FaceTop)
	w.ExpectBlockAck()
	if name, props := servertest.StatePropsOf(t, srv, 8, 64, 0); name != "minecraft:barrel" || props["facing"] != "up" {
		t.Errorf("the barrel stands as %s %v, want one facing up under a downward look", name, props)
	}

	// The dropper reads its look the same way: level and east, and it
	// faces west.
	w.SetCreativeSlot(36, servertest.ItemStack(t, "minecraft:dropper", 1))
	w.FacePlayer(2.5, 64, 4.5, 270, 0)
	w.PlaceBlock(4, 63, 4, protocol.FaceTop)
	w.ExpectBlockAck()
	if name, props := servertest.StatePropsOf(t, srv, 4, 64, 4); name != "minecraft:dropper" || props["facing"] != "west" {
		t.Errorf("the dropper stands as %s %v, want one facing west", name, props)
	}
}

// TestBreakingAShulkerBoxDropsItWithWhatItHeld checks the one container
// whose contents survive their block's break: the shulker box's loot table
// copies what the block entity kept into the box item it drops, the
// minecraft:container component, and nothing spills beside it. A chest's
// contents spill and the chest drops empty; a shulker's ride inside.
func TestBreakingAShulkerBoxDropsItWithWhatItHeld(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.SurvivalConfig(), "Packrat", servertest.OpsListing(t, "Packrat"))

	// The box on the ground two blocks from the player, set down the way a
	// survival player builds: the stack from their own hand.
	const bx, by, bz = int32(2), int32(64), int32(2)
	box, ok := registry.ItemID("minecraft:shulker_box")
	if !ok {
		t.Fatal("the item registry has no shulker box")
	}
	srv.GiveSlot("Packrat", player.HotbarSlot, protocol.ItemStack{ItemID: box, Count: 1})
	c.MoveStep(0.5, 64, 0.5, true)
	c.PlaceBlock(bx, by-1, bz, protocol.FaceTop)
	if seq := c.ExpectBlockAck(); seq != 1 {
		t.Errorf("acknowledged sequence = %d, want 1", seq)
	}
	// The placement's own block update to the placer rides behind the ack
	// at its own pace; collected here so the break's settled read below
	// starts from a quiet line. On a slow machine it can still come later,
	// which is why the break's result is read off the world below.
	c.SettleAndCollect(func(int32, []byte) {})
	if name, _ := servertest.StatePropsOf(t, srv, bx, by, bz); name != "minecraft:shulker_box" {
		t.Fatalf("the box stands as %s, want a shulker box", name)
	}

	// Two things in it, in slots three and thirteen, so the slots they sat
	// in ride along with them.
	dirt, ok := registry.ItemID("minecraft:dirt")
	if !ok {
		t.Fatal("the item registry has no dirt")
	}
	stick, ok := registry.ItemID("minecraft:stick")
	if !ok {
		t.Fatal("the item registry has no stick")
	}
	if !srv.SetInventoryAt(bx, by, bz, 3, protocol.ItemStack{ItemID: dirt, Count: 8}) {
		t.Fatal("the shulker box has no slot three to fill")
	}
	if !srv.SetInventoryAt(bx, by, bz, 13, protocol.ItemStack{ItemID: stick, Count: 4}) {
		t.Fatal("the shulker box has no slot thirteen to fill")
	}

	// A pickaxe in the hand, the tool the box gives its contents up to.
	pickaxe, ok := registry.ItemID("minecraft:iron_pickaxe")
	if !ok {
		t.Fatal("the item registry has no iron pickaxe")
	}
	srv.GiveSlot("Packrat", player.HotbarSlot, protocol.ItemStack{ItemID: pickaxe, Count: 1})

	// The break settles as one: the box gone from the world, and the box
	// item out of it carrying the two stacks inside it.
	c.BreakBlock(bx, by, bz)
	change, item := c.WaitBreakSettled()
	if change.X != bx || change.Y != by || change.Z != bz {
		t.Errorf("block update at (%d, %d, %d), want the box that was broken", change.X, change.Y, change.Z)
	}
	if got := srv.BlockAt(bx, by, bz); got != 0 {
		t.Errorf("block after the break = %d, want 0 (air)", got)
	}
	if item.Stack.ItemID != box || item.Stack.Count != 1 {
		t.Errorf("the drop = %+v, want one shulker box (item %d)", item.Stack, box)
	}
	held := item.Stack.Container
	if len(held) != 14 {
		t.Fatalf("the drop's container holds %d entries, want the fourteen the last full slot runs to", len(held))
	}
	if held[3].ItemID != dirt || held[3].Count != 8 {
		t.Errorf("slot three of the drop = %+v, want eight dirt (item %d)", held[3], dirt)
	}
	if held[13].ItemID != stick || held[13].Count != 4 {
		t.Errorf("slot thirteen of the drop = %+v, want four sticks (item %d)", held[13], stick)
	}
	for i, stack := range held {
		if i != 3 && i != 13 && !stack.Empty() {
			t.Errorf("slot %d of the drop = %+v, want it empty", i, stack)
		}
	}

	// And nothing spilled beside the box: the break leaves one item
	// entity, the box itself.
	c.Settle()
	items := srv.ItemsInDimension("minecraft:overworld")
	if len(items) != 1 || items[0].Item != "minecraft:shulker_box" {
		t.Errorf("the items the break left = %+v, want the shulker box alone", items)
	}
	if got := srv.InventoryAt(bx, by, bz); got != nil {
		t.Errorf("the shulker box still keeps slots after its break: %v", got)
	}
}

// TestAnOpenContainersLidIsReaskedEveryFiveTicks checks the lid's recheck:
// vanilla's counter schedules a block tick five ticks out at every send and
// answers it with the block action again, so an open container's lid count
// repeats every five ticks for as long as somebody is looking in, and stops
// once the last viewer has gone.
func TestAnOpenContainersLidIsReaskedEveryFiveTicks(t *testing.T) {
	t.Parallel()
	srv, w := servertest.OpenWindowed(t, servertest.BlockTestConfig())

	w.SetCreativeSlot(36, servertest.ItemStack(t, "minecraft:chest", 1))
	w.PlaceBlock(4, 63, 0, protocol.FaceTop)
	w.ExpectBlockAck()

	// The chest opened: the lid goes out with one viewer looking in.
	w.UseBlock(4, 64, 0)
	w.ExpectContainerOpen(protocol.MenuGeneric9x3, true, 4, 64, 0)
	if action, param, block := w.ExpectLidAction(4, 64, 0); action != protocol.ChestActionLid || param != 1 || block != protocol.BlockChest {
		t.Errorf("the opening lid: action=%d param=%d block=%d, want lid 1 chest %d", action, param, block, protocol.BlockChest)
	}

	// Five ticks of the loop, and the lid's count comes again.
	servertest.AwaitTicks(t, srv, 5)
	if action, param, _ := w.ExpectLidAction(4, 64, 0); action != protocol.ChestActionLid || param != 1 {
		t.Errorf("the first recheck: action=%d param=%d, want lid 1", action, param)
	}

	// Five more, and again: the recheck books itself while viewers remain.
	servertest.AwaitTicks(t, srv, 5)
	if action, param, _ := w.ExpectLidAction(4, 64, 0); action != protocol.ChestActionLid || param != 1 {
		t.Errorf("the second recheck: action=%d param=%d, want lid 1", action, param)
	}

	// The window closed: the lid falls with the close, the recheck that
	// was already booked falls it once more, and then the rechecks stop.
	w.CloseWindow()
	w.ExpectWindowZero()
	// Between the close being sent and handled the tick loop can reask
	// the lid with the viewer still counted any number of times; skip
	// each lid 1 until the close's own lid 0.
	action, param, _ := w.ExpectLidAction(4, 64, 0)
	for action == protocol.ChestActionLid && param == 1 {
		action, param, _ = w.ExpectLidAction(4, 64, 0)
	}
	if action != protocol.ChestActionLid || param != 0 {
		t.Errorf("the closing lid: action=%d param=%d, want lid 0", action, param)
	}
	var rechecks int
	servertest.AwaitTicks(t, srv, 6)
	w.SettleAndCollect(func(id int32, data []byte) {
		if id == protocol.IDPlayBlockAction {
			rechecks++
		}
	})
	if rechecks != 1 {
		t.Errorf("the closed chest sent %d lid actions after the close, want the one stale recheck the already-booked tick gives", rechecks)
	}
}

// TestASecondViewerDoesNotPushBackAChestsLidRecheck checks that a chest's
// five-tick lid recheck is booked by the first viewer and left where it is
// when a second one opens: vanilla's opener counter schedules the recheck
// only when the count goes from zero to one, and the scheduled tick is
// deduplicated per position, so a later open never delays it.
func TestASecondViewerDoesNotPushBackAChestsLidRecheck(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.BlockTestConfig())

	// Bob puts the chest out alone, and Alice arrives after him.
	bob := servertest.WindowedOf(t, "Bob", connect())
	bob.SetCreativeSlot(36, servertest.ItemStack(t, "minecraft:chest", 1))
	bob.PlaceBlock(4, 63, 0, protocol.FaceTop)
	bob.ExpectBlockAck()

	alice := servertest.WindowedOf(t, "Alice", connect())
	bob.ExpectPlayerAdded("Alice")
	bob.ExpectAnnouncement("multiplayer.player.joined", "Alice")

	// The ticks are the test's from here: the main loop is stopped so each
	// turn of the block entities that walks the recheck is counted.
	srv.StopTick()

	// Alice opens first: the lid tells one viewer and books the recheck
	// five ticks out.
	alice.UseBlock(4, 64, 0)
	alice.ExpectContainerOpen(protocol.MenuGeneric9x3, true, 4, 64, 0)
	alice.ExpectLid(4, 64, 0, 1)
	bob.ExpectLid(4, 64, 0, 1)

	// Three of the five ticks pass, and Bob opens the same chest.
	servertest.TickBlockEntitiesByHand(srv, 3)
	bob.UseBlock(4, 64, 0)
	bob.ExpectContainerOpen(protocol.MenuGeneric9x3, true, 4, 64, 0)
	alice.ExpectLid(4, 64, 0, 2)
	bob.ExpectLid(4, 64, 0, 2)

	// Two ticks finish the five Alice booked: the lid repeats with both
	// viewers rather than waiting a fresh five from Bob's open.
	servertest.TickBlockEntitiesByHand(srv, 2)
	if action, param, block := bob.ExpectLidAction(4, 64, 0); action != protocol.ChestActionLid || param != 2 || block != protocol.BlockChest {
		t.Errorf("the recheck after the second open: action=%d param=%d block=%d, want lid 2 chest %d", action, param, block, protocol.BlockChest)
	}
}

// TestALaterViewerBooksAChestsLidRecheck checks that an open whose viewer
// count is already above one still books the recheck when none is pending -
// the state two opens on different connections leave when each counts the
// other's window - so the lid keeps repeating while anyone looks in.
func TestALaterViewerBooksAChestsLidRecheck(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.BlockTestConfig())

	// Bob puts the chest out alone, and Alice arrives after him.
	bob := servertest.WindowedOf(t, "Bob", connect())
	bob.SetCreativeSlot(36, servertest.ItemStack(t, "minecraft:chest", 1))
	bob.PlaceBlock(4, 63, 0, protocol.FaceTop)
	bob.ExpectBlockAck()

	alice := servertest.WindowedOf(t, "Alice", connect())
	bob.ExpectPlayerAdded("Alice")
	bob.ExpectAnnouncement("multiplayer.player.joined", "Alice")

	// The ticks are the test's from here, so each turn of the block
	// entities that walks the recheck is counted.
	srv.StopTick()

	// Alice opens the chest alone; the booking her open made is dropped
	// to stand in for the race, leaving both of them looking in with no
	// recheck due.
	alice.UseBlock(4, 64, 0)
	alice.ExpectContainerOpen(protocol.MenuGeneric9x3, true, 4, 64, 0)
	alice.ExpectLid(4, 64, 0, 1)
	bob.ExpectLid(4, 64, 0, 1)
	srv.ClearLidRecheck(4, 64, 0)

	// Bob's open counts two viewers, and with no recheck pending its
	// booking is the only one that can repeat the lid.
	bob.UseBlock(4, 64, 0)
	bob.ExpectContainerOpen(protocol.MenuGeneric9x3, true, 4, 64, 0)
	alice.ExpectLid(4, 64, 0, 2)
	bob.ExpectLid(4, 64, 0, 2)

	// Five ticks out, the booking fires and repeats the lid with both
	// viewers.
	servertest.TickBlockEntitiesByHand(srv, 5)
	if action, param, block := bob.ExpectLidAction(4, 64, 0); action != protocol.ChestActionLid || param != 2 || block != protocol.BlockChest {
		t.Errorf("the recheck booked by a later viewer: action=%d param=%d block=%d, want lid 2 chest %d", action, param, block, protocol.BlockChest)
	}
}

// TestAChestOpenedAndShutInAFlurryWhileTheTickRunsKeepsItsLid rushes a chest
// open and shut again and again from the player's connection while the tick
// loop walks the booked rechecks between the bursts - the writes and the
// walks living on different goroutines, the way they do on a busy server.
// Nothing comes down, and the lid still falls with the last viewer and stops
// being re-asked.
func TestAChestOpenedAndShutInAFlurryWhileTheTickRunsKeepsItsLid(t *testing.T) {
	t.Parallel()
	srv, w := servertest.OpenWindowed(t, servertest.BlockTestConfig())

	w.SetCreativeSlot(36, servertest.ItemStack(t, "minecraft:chest", 1))
	w.PlaceBlock(4, 63, 0, protocol.FaceTop)
	w.ExpectBlockAck()

	// The chest opened once, so a recheck sits booked for the tick to
	// walk while the flurry goes on.
	w.UseBlock(4, 64, 0)
	w.ExpectContainerOpen(protocol.MenuGeneric9x3, true, 4, 64, 0)
	w.ExpectLid(4, 64, 0, 1)

	// The flurry: bursts of right-clicks - each one shuts the window it
	// finds and opens the chest again - with the tick running between the
	// bursts and the answers drained as they come.
	for range 10 {
		for range 4 {
			w.UseBlock(4, 64, 0)
		}
		servertest.AwaitTicks(t, srv, 1)
		w.Drain()
	}

	// The flurry leaves the chest open and one recheck booked; five ticks
	// let it fire and rebook the way an open chest's does.
	servertest.AwaitTicks(t, srv, 5)

	// The player looks away, and the lid falls with the close.
	w.CloseWindow()
	w.ExpectWindowZero()
	w.ExpectLid(4, 64, 0, 0)

	// The already-booked recheck answers once more with nobody left, and
	// then the rechecks stop.
	var stale int
	servertest.AwaitTicks(t, srv, 6)
	w.SettleAndCollect(func(id int32, data []byte) {
		if id != protocol.IDPlayBlockAction {
			return
		}
		r := protocol.NewReader(data)
		x, y, z := r.ReadPosition()
		if x == 4 && y == 64 && z == 0 {
			stale++
		}
	})
	if stale != 1 {
		t.Errorf("the closed chest sent %d lid actions after the close, want the one stale recheck the already-booked tick gives", stale)
	}
}

// TestABarrelTellsItsLidBySoundAlone checks the barrel's lid: the vanilla
// barrel's openers counter answers its count change with nothing at all -
// no block action ever rides it - so a barrel's lid is told by its open
// and close sounds and nothing else.
func TestABarrelTellsItsLidBySoundAlone(t *testing.T) {
	t.Parallel()
	srv, w := servertest.OpenWindowed(t, servertest.BlockTestConfig())

	w.SetCreativeSlot(36, servertest.ItemStack(t, "minecraft:barrel", 1))
	w.PlaceBlock(4, 63, 0, protocol.FaceTop)
	w.ExpectBlockAck()

	w.UseBlock(4, 64, 0)
	// The open sound goes out while the window is built, before Open Screen.
	name, category, sx, sy, sz, volume, pitch := w.ExpectContainerSound()
	w.ExpectContainerOpenType(protocol.MenuGeneric9x3, protocol.BlockEntityBarrel, 4, 64, 0)
	if name != "minecraft:block.barrel.open" {
		t.Errorf("the barrel opened to the sound %q, want minecraft:block.barrel.open", name)
	}
	if category != protocol.SoundCategoryBlock {
		t.Errorf("the barrel's open sound played in category %d, want block %d", category, protocol.SoundCategoryBlock)
	}
	if sx < 3.9 || sx > 5.1 || sy < 63.9 || sy > 65.1 || sz < -0.1 || sz > 1.1 {
		t.Errorf("the barrel's open sound stood at (%g, %g, %g), want it by the barrel at (4, 64, 0)", sx, sy, sz)
	}
	if volume != 0.5 {
		t.Errorf("the barrel's open sound played at volume %g, want 0.5", volume)
	}
	if pitch < 0.9 || pitch > 1.0 {
		t.Errorf("the barrel's open sound played at pitch %g, want the vanilla roll between 0.9 and 1.0", pitch)
	}

	// The barrel says nothing by block action, for as long as it stays
	// open and after it closes.
	w.ExpectNoLidAction(srv, 12, 4, 64, 0)

	w.CloseWindow()
	w.ExpectWindowZero()
	name, _, _, _, _, _, _ = w.ExpectContainerSound()
	if name != "minecraft:block.barrel.close" {
		t.Errorf("the barrel closed to the sound %q, want minecraft:block.barrel.close", name)
	}
	w.ExpectNoLidAction(srv, 6, 4, 64, 0)
}

// TestAShulkerLidSpeaksOncePerOpenAndClose checks the shulker box's lid:
// vanilla's box tells the lid once per open and once per close - no
// recheck repeats it while somebody looks in - and each colour of box
// names its own block in the action.
func TestAShulkerLidSpeaksOncePerOpenAndClose(t *testing.T) {
	t.Parallel()
	srv, w := servertest.OpenWindowed(t, servertest.BlockTestConfig())

	w.SetCreativeSlot(36, servertest.ItemStack(t, "minecraft:shulker_box", 1))
	w.PlaceBlock(4, 63, 0, protocol.FaceTop)
	w.ExpectBlockAck()

	w.UseBlock(4, 64, 0)
	// The open sound goes out while the window is built, before Open Screen.
	name, _, _, _, _, _, _ := w.ExpectContainerSound()
	w.ExpectContainerOpenType(protocol.MenuShulkerBox, protocol.BlockEntityShulkerBox, 4, 64, 0)
	if name != "minecraft:block.shulker_box.open" {
		t.Errorf("the shulker opened to the sound %q, want minecraft:block.shulker_box.open", name)
	}
	if action, param, block := w.ExpectLidAction(4, 64, 0); action != protocol.ChestActionLid || param != 1 || block != protocol.BlockShulkerBoxIDs["shulker_box"] {
		t.Errorf("the opening lid: action=%d param=%d block=%d, want lid 1 shulker %d", action, param, block, protocol.BlockShulkerBoxIDs["shulker_box"])
	}

	// The shulker's lid count does not repeat: the box's counter books no
	// recheck, so the one action per open is all an open look gets.
	w.ExpectNoLidAction(srv, 12, 4, 64, 0)

	w.CloseWindow()
	w.ExpectWindowZero()
	name, _, _, _, _, _, _ = w.ExpectContainerSound()
	if name != "minecraft:block.shulker_box.close" {
		t.Errorf("the shulker closed to the sound %q, want minecraft:block.shulker_box.close", name)
	}
	if action, param, block := w.ExpectLidAction(4, 64, 0); action != protocol.ChestActionLid || param != 0 || block != protocol.BlockShulkerBoxIDs["shulker_box"] {
		t.Errorf("the closing lid: action=%d param=%d block=%d, want lid 0 shulker %d", action, param, block, protocol.BlockShulkerBoxIDs["shulker_box"])
	}
	// And nothing follows the close: no stale recheck to fall the lid a
	// second time.
	w.ExpectNoLidAction(srv, 6, 4, 64, 0)
}

// TestCreativeBreakingAShulkerBoxDropsItOnlyWhenItHoldsSomething checks the
// one drop a creative break keeps: a non-empty shulker box comes out with its
// contents, an empty one drops nothing.
func TestCreativeBreakingAShulkerBoxDropsItOnlyWhenItHoldsSomething(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Packrat")
	c.MoveStep(0.5, 64, 0.5, true)

	box, ok := registry.ItemID("minecraft:shulker_box")
	if !ok {
		t.Fatal("the item registry has no shulker box")
	}
	dirt, ok := registry.ItemID("minecraft:dirt")
	if !ok {
		t.Fatal("the item registry has no dirt")
	}
	c.SetCreativeSlot(player.HotbarSlot, protocol.ItemStack{ItemID: box, Count: 1})
	for _, bx := range []int32{2, 4} {
		c.PlaceBlock(bx, 63, 2, protocol.FaceTop)
		c.ExpectBlockAck()
		c.SettleAndCollect(func(int32, []byte) {})
		if name, _ := servertest.StatePropsOf(t, srv, bx, 64, 2); name != "minecraft:shulker_box" {
			t.Fatalf("the box at x=%d stands as %s, want a shulker box", bx, name)
		}
	}
	if !srv.SetInventoryAt(2, 64, 2, 3, protocol.ItemStack{ItemID: dirt, Count: 8}) {
		t.Fatal("the shulker box has no slot three to fill")
	}

	// Out of the item's pickup reach, which a creative player voids stacks in.
	c.MoveStep(-2.5, 64, 0.5, true)
	c.Settle()

	// The empty box first: nothing drops.
	c.StartDiggingHere(4, 64, 2)
	c.Settle()
	if items := srv.ItemsInDimension("minecraft:overworld"); len(items) != 0 {
		t.Errorf("the empty box's break left %+v, want nothing", items)
	}

	c.StartDiggingHere(2, 64, 2)
	c.Settle()
	items := srv.ItemsInDimension("minecraft:overworld")
	if len(items) != 1 || items[0].Item != "minecraft:shulker_box" {
		t.Fatalf("the full box's break left %+v, want the shulker box alone", items)
	}
	if got := items[0].Stack.Container; len(got) != 4 || got[3].ItemID != dirt || got[3].Count != 8 {
		t.Errorf("the drop holds %+v, want eight dirt in slot three", got)
	}
}

// TestCreativeShulkerBoxDropAppearsBeforeTheBreakEvent checks what a bystander
// sees of a creative break: the box's item spawns before the 2001 break event.
func TestCreativeShulkerBoxDropAppearsBeforeTheBreakEvent(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Packrat")
	c.MoveStep(0.5, 64, 0.5, true)

	obs := servertest.DialServer(t, srv).Join("Watcher")
	obs.Settle()
	c.Settle()

	box, ok := registry.ItemID("minecraft:shulker_box")
	if !ok {
		t.Fatal("the item registry has no shulker box")
	}
	dirt, ok := registry.ItemID("minecraft:dirt")
	if !ok {
		t.Fatal("the item registry has no dirt")
	}
	c.SetCreativeSlot(player.HotbarSlot, protocol.ItemStack{ItemID: box, Count: 1})
	c.PlaceBlock(2, 63, 2, protocol.FaceTop)
	c.ExpectBlockAck()
	c.Settle()
	if !srv.SetInventoryAt(2, 64, 2, 3, protocol.ItemStack{ItemID: dirt, Count: 8}) {
		t.Fatal("the shulker box has no slot three to fill")
	}
	obs.Settle()

	c.StartDiggingHere(2, 64, 2)
	c.Settle()
	var order []string
	obs.SettleAndCollect(func(id int32, data []byte) {
		switch id {
		case protocol.IDPlaySpawnEntity:
			if _, typ := servertest.SpawnEntityHead(data); typ == protocol.EntityTypeItem {
				order = append(order, "item")
			}
		case protocol.IDPlayWorldEvent:
			if protocol.NewReader(data).ReadI32() == protocol.WorldEventBlockBreak {
				order = append(order, "break")
			}
		}
	})
	if len(order) != 2 || order[0] != "item" || order[1] != "break" {
		t.Errorf("the bystander saw %v, want the item then the break event", order)
	}
}
