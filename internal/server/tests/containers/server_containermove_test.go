package containers

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"strconv"
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
	farmland, ok := registry.FindState("minecraft:farmland", map[string]string{"moisture": "7"})
	if !ok {
		t.Fatal("the registry holds no farmland")
	}
	srv.SetLoadedBlock(4, 63, 1, farmland)
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
