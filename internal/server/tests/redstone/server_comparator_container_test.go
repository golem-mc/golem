package redstone

// vanilla: BlockEntity.setChanged, Level.updateNeighbourForOutputSignal,
// CompoundContainer.setChanged, ComparatorBlock.newBlockEntity

import (
	"testing"
	"time"

	"github.com/golem-mc/golem/internal/server/servertest"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// comparatorPropsOf reads a comparator's parsed properties.
func comparatorPropsOf(t *testing.T, srv *server.Server, x, y, z int32) map[string]string {
	t.Helper()
	name, props := servertest.StatePropsOf(t, srv, x, y, z)
	if name != "minecraft:comparator" {
		t.Fatalf("the block at (%d, %d, %d) is %s, want a comparator", x, y, z, name)
	}
	return props
}

// placeAComparatorNorthOfHalf places a comparator one block north of the
// chest half at (x, y, z), clicking the half's north face while sneaking so
// the click builds instead of opening the pair. The player looks north, so
// the comparator faces south, its input the half.
func placeAComparatorNorthOfHalf(t *testing.T, w *servertest.WindowedClient, srv *server.Server, x, y, z int32) {
	t.Helper()
	w.FacePlayer(float64(x)+0.5, 64, 3.5, 180, 0)
	w.Sneak(true)
	w.SetCreativeSlot(36, servertest.ItemStack(t, "minecraft:comparator", 1))
	w.PlaceBlock(x, y, z, protocol.FaceNorth)
	w.Sneak(false)
	w.ExpectBlockAck()
	if name, props := servertest.StatePropsOf(t, srv, x, y, z-1); name != "minecraft:comparator" || props["facing"] != "south" {
		t.Fatalf("the comparator north of (%d, %d, %d) is %s %v, want one facing south", x, y, z, name, props)
	}
}

// TestComparatorFollowsADoubleChestChangedThroughItsWindow puts a comparator
// beside each half of a double chest, fills the pair, and then moves more
// through its window: both comparators read the pair as one, so both wires
// rise with the change even though the click wrote to a single half.
// vanilla: Level.updateNeighbourForOutputSignal, CompoundContainer.setChanged
func TestComparatorFollowsADoubleChestChangedThroughItsWindow(t *testing.T) {
	t.Parallel()
	srv, w := servertest.OpenWindowed(t, servertest.BlockTestConfig())

	servertest.PlaceChestPair(t, w, "minecraft:chest", 4, 64, 0)

	// Fifteen full stacks in the left half: a pair of 54 slots reads
	// floor(15/54*14)+1 = 4.
	dirt := servertest.ItemStack(t, "minecraft:dirt", 64)
	for slot := range 15 {
		if !srv.SetInventoryAt(4, 64, 0, slot, dirt) {
			t.Fatalf("the left half has no slot %d", slot)
		}
	}
	// A wire beyond each comparator, laid first so the comparator's own
	// tick wakes it rather than racing its placement.
	wire, _ := registry.FindState("minecraft:redstone_wire", nil)
	if !srv.SetBlock(4, 64, -2, wire) || !srv.SetBlock(5, 64, -2, wire) {
		t.Fatal("the wires' chunks are not loaded")
	}
	placeAComparatorNorthOfHalf(t, w, srv, 4, 64, 0)
	placeAComparatorNorthOfHalf(t, w, srv, 5, 64, 0)
	servertest.ExpectWirePower(t, srv, 4, 64, -2, "4")
	servertest.ExpectWirePower(t, srv, 5, 64, -2, "4")

	// Four more stacks through the window: the pair holds 19, which reads
	// floor(19/54*14)+1 = 5.
	w.UseBlock(4, 64, 0)
	w.ExpectContainerOpen(protocol.MenuGeneric9x6, true, 4, 64, 0)
	for windowSlot := range int16(4) {
		w.SetCreativeSlot(36, dirt)
		w.Click(81, 0, protocol.ClickModePickup)
		w.ExpectClickResult(map[int16]protocol.ItemStack{81: {}}, dirt)
		w.Click(windowSlot, 0, protocol.ClickModePickup)
		w.ExpectClickResult(map[int16]protocol.ItemStack{windowSlot: dirt}, protocol.ItemStack{})
	}

	servertest.ExpectWirePower(t, srv, 4, 64, -2, "5")
	servertest.ExpectWirePower(t, srv, 5, 64, -2, "5")
}

// TestAComparatorsReadingADoubleChestFollowsAReplacedHalf moves contents into
// a full pair through its window, breaks one half and puts it back: the
// surviving half reads as a single while its partner is gone, and the pair
// reads as one again once it returns, so both comparators follow the change.
// vanilla: CompoundContainer.setChanged
func TestAComparatorsReadingADoubleChestFollowsAReplacedHalf(t *testing.T) {
	t.Parallel()
	srv, w := servertest.OpenWindowed(t, servertest.BlockTestConfig())

	servertest.PlaceChestPair(t, w, "minecraft:chest", 4, 64, 0)
	dirt := servertest.ItemStack(t, "minecraft:dirt", 64)
	for slot := range 15 {
		if !srv.SetInventoryAt(4, 64, 0, slot, dirt) {
			t.Fatalf("the left half has no slot %d", slot)
		}
	}
	// The left comparator's wire, laid first so the comparator's own tick
	// wakes it rather than racing its placement.
	wire, _ := registry.FindState("minecraft:redstone_wire", nil)
	if !srv.SetBlock(4, 64, -2, wire) {
		t.Fatal("the wire's chunk is not loaded")
	}
	placeAComparatorNorthOfHalf(t, w, srv, 4, 64, 0)
	placeAComparatorNorthOfHalf(t, w, srv, 5, 64, 0)
	servertest.ExpectWirePower(t, srv, 4, 64, -2, "4")
	// One more stack into the left half through the window (its slots are
	// 27-53, and its own first fifteen are full): the pair holds 16, which
	// reads floor(16/54*14)+1 = 5, and both comparators hear the change.
	w.UseBlock(4, 64, 0)
	w.ExpectContainerOpen(protocol.MenuGeneric9x6, true, 4, 64, 0)
	w.SetCreativeSlot(36, dirt)
	w.Click(81, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{81: {}}, dirt)
	w.Click(42, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{42: dirt}, protocol.ItemStack{})
	servertest.ExpectWirePower(t, srv, 4, 64, -2, "5")
	servertest.UntilTicks(t, srv, 200, "condition", func() bool {
		return comparatorPropsOf(t, srv, 5, 64, -1)["powered"] == "true"
	})
	w.CloseWindow()
	w.ExpectWindowZero()

	// Break the empty right half: the left half is a single chest of 16
	// stacks, floor(16/27*14)+1 = 9, and the right comparator reads nothing.
	w.SetCreativeSlot(36, servertest.ItemStack(t, "minecraft:stone_pickaxe", 1))
	w.BreakBlock(5, 64, 0)
	w.ExpectBlockAck()
	servertest.ExpectWirePower(t, srv, 4, 64, -2, "9")
	servertest.UntilTicks(t, srv, 200, "condition", func() bool {
		return comparatorPropsOf(t, srv, 5, 64, -1)["powered"] == "false"
	})

	// Put the right half back: the pair reads 5 again, and both comparators
	// follow.
	w.FacePlayer(4.5, 64, 3.5, 0, 0)
	w.SetCreativeSlot(36, servertest.ItemStack(t, "minecraft:chest", 1))
	w.PlaceBlock(5, 63, 0, protocol.FaceTop)
	w.ExpectBlockAck()
	servertest.ExpectWirePower(t, srv, 4, 64, -2, "5")
	servertest.UntilTicks(t, srv, 200, "condition", func() bool {
		return comparatorPropsOf(t, srv, 5, 64, -1)["powered"] == "true"
	})
}

// TestAComparatorSeesAContainerChangeTwoTicksLater pins the tick the wire
// changes on, not only that it changes: a hopper move into a chest schedules
// the comparator reading the chest for two ticks after the move, so the wire
// is quiet on the move's own tick and the one after, and changes on the
// second. A change drained a tick after it was made must not add that tick to
// the comparator's delay.
// vanilla: Level.updateNeighbourForOutputSignal
func TestAComparatorSeesAContainerChangeTwoTicksLater(t *testing.T) {
	t.Parallel()
	clock := servertest.NewStepClock()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Watcher", func(s *server.Server) {
		s.SetTickClock(clock, nil)
		s.SetKeepAlive(time.Hour, time.Hour)
	}, servertest.OpsListing(t, "Watcher"))
	c.Settle()

	// A hopper feeding a chest, a comparator reading the chest, and the wire
	// the comparator drives. The placements' own ticks settle before the
	// item goes into the hopper, so the move below is the only wake.
	servertest.Setblock(t, c, 4, 64, 0, "minecraft:chest")
	servertest.Setblock(t, c, 4, 65, 0, "minecraft:hopper[facing=down]")
	wire, _ := registry.FindState("minecraft:redstone_wire", nil)
	if !srv.SetBlock(4, 64, -2, wire) {
		t.Fatal("the wire's chunk is not loaded")
	}
	servertest.Setblock(t, c, 4, 64, -1, "minecraft:comparator[facing=south,mode=compare,powered=false]")
	servertest.StepTicks(t, srv, clock, 5)
	if got := servertest.WirePowerAt(srv, 4, 64, -2); got != "0" {
		t.Fatalf("the comparator drives the wire to %q with the chest empty, want 0", got)
	}
	if !srv.SetInventoryAt(4, 65, 0, 0, servertest.ItemStack(t, "minecraft:dirt", 1)) {
		t.Fatal("the hopper has no slot 0 to fill")
	}

	// The move lands on the first tick; the comparator's delay is two ticks
	// from there, so the wire is still quiet on the move's tick and the
	// next, and changes on the second.
	servertest.StepTicks(t, srv, clock, 1)
	if got := servertest.WirePowerAt(srv, 4, 64, -2); got != "0" {
		t.Fatalf("the wire changed on the move's own tick (power %q), want 0", got)
	}
	servertest.StepTicks(t, srv, clock, 1)
	if got := servertest.WirePowerAt(srv, 4, 64, -2); got != "0" {
		t.Fatalf("the wire changed one tick after the move (power %q), want 0", got)
	}
	servertest.StepTicks(t, srv, clock, 1)
	if got := servertest.WirePowerAt(srv, 4, 64, -2); got != "1" {
		t.Fatalf("two ticks after the move the wire reads %q, want 1", got)
	}
}

// TestAComparatorSetBySetblockKeepsItsOutputSignal sets a comparator down
// with /setblock beside a chest: the command gives it the block entity its
// output is saved in. A neighbour change makes it compute the chest's read;
// breaking the chest takes the read away and the comparator turns off.
// vanilla: ComparatorBlock.newBlockEntity
func TestAComparatorSetBySetblockKeepsItsOutputSignal(t *testing.T) {
	t.Parallel()
	srv, c := servertest.AttachActor(t)

	servertest.Setblock(t, c, 4, 64, 0, "minecraft:chest")
	if !srv.SetInventoryAt(4, 64, 0, 0, servertest.ItemStack(t, "minecraft:dirt", 64)) {
		t.Fatal("the chest has no block entity to fill")
	}
	// The wire, laid first so the comparator's own tick wakes it rather
	// than racing its placement.
	wire, _ := registry.FindState("minecraft:redstone_wire", nil)
	if !srv.SetBlock(6, 64, 0, wire) {
		t.Fatal("the wire's chunk is not loaded")
	}
	servertest.Setblock(t, c, 5, 64, 0, "minecraft:comparator[facing=west,mode=compare,powered=false]")
	servertest.Setblock(t, c, 5, 64, 1, "stone")

	servertest.ExpectWirePower(t, srv, 6, 64, 0, "1")

	servertest.Setblock(t, c, 4, 64, 0, "air")
	servertest.UntilTicks(t, srv, 200, "condition", func() bool {
		return comparatorPropsOf(t, srv, 5, 64, 0)["powered"] == "false"
	})
	servertest.ExpectWirePower(t, srv, 6, 64, 0, "0")
}

// wirePowerInDimension is wirePowerAt over the named dimension, reading a
// wire's power where the block there is one.
func wirePowerInDimension(srv *server.Server, dimension string, x, y, z int32) string {
	name, props, ok := registry.BlockStateName(srv.BlockInDimension(dimension, x, y, z))
	if !ok || name != "minecraft:redstone_wire" {
		return ""
	}
	return registry.ParseStateProps(props)["power"]
}

// TestAComparatorInTheNetherSeesAContainerChangeTwoTicksLater is the exact-
// tick test over a dimension whose own age never advances: a hopper feeding a
// chest in the nether still schedules the comparator reading the chest for
// two ticks after the move, from the overworld's clock. Stamped with the
// dimension's always-zero age instead, the move is already overdue and the
// comparator fires on the next physics tick.
func TestAComparatorInTheNetherSeesAContainerChangeTwoTicksLater(t *testing.T) {
	t.Parallel()
	srv, _ := servertest.OpenWindowed(t, servertest.BlockTestConfig())
	srv.StopTick()

	// Forced chunks run scheduled ticks and block entities with no player near.
	srv.ForceChunkInDimension(world.NetherName, 0, 0)
	srv.ForceChunkInDimension(world.NetherName, 0, -1)

	const dim = world.NetherName
	chest, _ := registry.FindState("minecraft:chest", nil)
	hopper, _ := registry.FindState("minecraft:hopper", map[string]string{"facing": "down"})
	wire, _ := registry.FindState("minecraft:redstone_wire", nil)
	comparator, _ := registry.FindState("minecraft:comparator",
		map[string]string{"facing": "south", "mode": "compare", "powered": "false"})

	place := func(x, y, z int32, state uint16, kind world.BlockEntityKind) {
		t.Helper()
		if !srv.SetBlockInDimension(dim, x, y, z, state) ||
			!srv.EnsureBlockEntityInDimension(dim, x, y, z, kind) {
			t.Fatalf("the %s did not stand at (%d, %d, %d) in the nether", kind, x, y, z)
		}
	}
	place(4, 64, 0, chest, world.KindChest)
	place(4, 65, 0, hopper, world.KindHopper)
	place(4, 64, -1, comparator, world.KindComparator)
	if !srv.SetBlockInDimension(dim, 4, 64, -2, wire) {
		t.Fatal("the wire did not stand in the nether")
	}
	if !srv.SetInventoryAtInDimension(dim, 4, 65, 0, 0, servertest.ItemStack(t, "minecraft:dirt", 1)) {
		t.Fatal("the hopper has no slot 0 to fill")
	}

	// The physics pass drains the hopper's move a tick after it is made; the
	// comparator's delay is two ticks from the move, so the wire is quiet on
	// the move's own tick and the one after, and changes on the second.
	srv.TickPhysics(1)
	srv.TickBlockEntities() // the move, stamped with the overworld's age
	if got := wirePowerInDimension(srv, dim, 4, 64, -2); got != "0" {
		t.Fatalf("the wire changed on the move's own tick (power %q), want 0", got)
	}
	srv.TickPhysics(1)
	if got := wirePowerInDimension(srv, dim, 4, 64, -2); got != "0" {
		t.Fatalf("the wire changed one tick after the move (power %q), want 0", got)
	}
	srv.TickPhysics(1)
	if got := wirePowerInDimension(srv, dim, 4, 64, -2); got != "1" {
		t.Fatalf("two ticks after the move the wire reads %q, want 1", got)
	}
}

// TestRemovingAContainerWakesAComparatorReadingItThroughStone fills a chest
// and a shelf, reads each through a stone block with a comparator, and
// removes the container with /setblock: the wire behind the comparator falls
// to zero.
// vanilla: Containers.updateNeighboursAfterDestroy, Level.updateNeighbourForOutputSignal
func TestRemovingAContainerWakesAComparatorReadingItThroughStone(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, block string }{
		{"chest", "chest"},
		{"shelf", "oak_shelf[facing=north]"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv, c := servertest.AttachActor(t)
			servertest.Setblock(t, c, 4, 64, 0, tc.block)
			if !srv.SetInventoryAt(4, 64, 0, 0, servertest.ItemStack(t, "minecraft:dirt", 64)) {
				t.Fatal("the container has no slot 0")
			}
			// The comparator and the wire need a support below them.
			stone, _ := registry.FindState("minecraft:stone", nil)
			srv.SetBlock(4, 63, 2, stone)
			srv.SetBlock(4, 63, 3, stone)
			servertest.Setblock(t, c, 4, 64, 3, "redstone_wire")
			servertest.Setblock(t, c, 4, 64, 2, "comparator[facing=north]")
			// The stone goes in last: its arrival wakes the comparator.
			servertest.Setblock(t, c, 4, 64, 1, "stone")
			servertest.ExpectWirePower(t, srv, 4, 64, 3, "1")
			c.ChatCommand("setblock 4 64 0 air")
			servertest.ExpectWirePower(t, srv, 4, 64, 3, "0")
		})
	}
}

// TestAComparatorReadsAChestOnlyWhenNothingSitsOnIt fills a chest and reads it
// with an adjacent comparator: a stone block above the chest makes it read 0.
// vanilla: ChestBlock.getAnalogOutputSignal, ChestBlock.isChestBlockedAt
func TestAComparatorReadsAChestOnlyWhenNothingSitsOnIt(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		blocked bool
		want    string
	}{
		{"open", false, "1"},
		{"blocked", true, "0"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv, c := servertest.AttachActor(t)
			servertest.Setblock(t, c, 4, 64, 0, "chest")
			if !srv.SetInventoryAt(4, 64, 0, 0, servertest.ItemStack(t, "minecraft:dirt", 64)) {
				t.Fatal("the chest has no slot 0")
			}
			stone, _ := registry.FindState("minecraft:stone", nil)
			srv.SetBlock(4, 63, 1, stone)
			srv.SetBlock(4, 63, 2, stone)
			servertest.Setblock(t, c, 4, 64, 2, "redstone_wire")
			if tc.blocked {
				servertest.Setblock(t, c, 4, 65, 0, "stone")
			}
			servertest.Setblock(t, c, 4, 64, 1, "comparator[facing=north]")
			servertest.Setblock(t, c, 5, 64, 1, "stone")
			// A comparator updates after a delay; settle before reading a 0.
			srv.TickPhysics(4)
			servertest.ExpectWirePower(t, srv, 4, 64, 2, tc.want)
		})
	}
}
