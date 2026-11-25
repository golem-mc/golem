package redstone

// vanilla: BlockEntity.setChanged, Level.updateNeighbourForOutputSignal,
// CompoundContainer.setChanged, ComparatorBlock.newBlockEntity

import (
	"fmt"
	"testing"
	"time"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

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

func TestDoubleChestComparatorWakeDependsOnHowTheSlotChanges(t *testing.T) {
	t.Parallel()
	const name = "Testificate"
	srv, c := servertest.Dial(t, servertest.BlockTestConfig(), servertest.OpsListing(t, name), func(s *server.Server) {
		servertest.FreezeUntilHeld(t, s)
	})
	w := servertest.WindowedOf(t, name, c)
	servertest.HoldTicks(t, srv)

	servertest.Setblock(t, c, 13, 64, 0, "chest[facing=north,type=left]")
	servertest.Setblock(t, c, 14, 64, 0, "chest[facing=north,type=right]")
	servertest.Setblock(t, c, 15, 64, 0, "stone")
	servertest.Setblock(t, c, 17, 64, 0, "redstone_wire")
	servertest.Setblock(t, c, 16, 64, 0, "comparator[facing=west]")
	servertest.RunTicks(t, srv, 4)

	expectPower := func(action string, powers ...string) {
		t.Helper()
		for tick, want := range powers {
			if tick > 0 {
				servertest.RunTicks(t, srv, 1)
			}
			if got := servertest.WirePowerAt(srv, 17, 64, 0); got != want {
				t.Fatalf("%s, tick %d: wire power = %q, want %q", action, tick, got, want)
			}
		}
	}
	dirt := servertest.ItemStack(t, "minecraft:dirt", 1)
	expectChestSlot := func(x int32, want protocol.ItemStack) {
		t.Helper()
		if slots := srv.InventoryAt(x, 64, 0); len(slots) == 0 || !slots[0].Equal(want) {
			t.Fatalf("chest at x=%d has slots %+v, want %+v in slot 0", x, slots, want)
		}
	}
	replace := func(x int32, item string, want protocol.ItemStack) {
		t.Helper()
		c.ChatCommand(fmt.Sprintf("item replace block %d 64 0 container.0 with %s", x, item))
		c.Settle()
		expectChestSlot(x, want)
	}
	replace(13, "minecraft:dirt", dirt)
	expectPower("command into the far half", "0", "0", "0", "0")

	replace(14, "minecraft:dirt", dirt)
	expectPower("command into the near half", "0", "0", "1")

	replace(13, "minecraft:air", protocol.ItemStack{})
	replace(14, "minecraft:air", protocol.ItemStack{})
	expectPower("clear both halves", "1", "1", "0")

	w.FacePlayer(13.5, 64, 3.5, 180, 0)
	w.SetCreativeSlot(36, dirt)
	w.UseBlock(13, 64, 0)
	w.ExpectContainerOpen(protocol.MenuGeneric9x6, true, 13, 64, 0)
	// Slot 27 is in the far half, which opens second in this pair.
	w.Click(27, 0, protocol.ClickModeSwap)
	w.ExpectClickResult(map[int16]protocol.ItemStack{27: dirt, 81: {}}, protocol.ItemStack{})
	expectChestSlot(13, dirt)
	expectChestSlot(14, protocol.ItemStack{})
	expectPower("window click into the far half", "0", "0", "1")
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
