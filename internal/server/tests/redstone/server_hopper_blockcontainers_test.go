package redstone

import (
	"slices"
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"

	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/registry"
)

// Hoppers moving through the container blocks no window covers, vanilla's
// getBlockContainer: the composter's two worldly views over the level its
// block state carries, and the one-slot decorated pot, the three-slot
// shelves and the copper chest, whose items their block entities hold. The
// flat world's ground stands at y=63, so the columns the tests build sit
// from y=64 up, and the cells below stand dug out.

// composterLevel returns the composter at a level, the way a test raises
// one, its items being the level alone.
func composterLevel(t *testing.T, level string) uint16 {
	t.Helper()
	return registry.MustState("minecraft:composter", map[string]string{"level": level})
}

// TestAHopperBelowAFullComposterDrainsIt checks the output container: a
// hopper under a level-8 composter takes its one bone meal through the
// composter's bottom face, which empties the composter back to level 0.
// One bone meal is all a full one holds: the next move, the cooldown
// spent, finds nothing to move.
func TestAHopperBelowAFullComposterDrainsIt(t *testing.T) {
	t.Parallel()
	srv, _ := servertest.OpenWindowed(t, servertest.BlockTestConfig())

	srv.SetLoadedBlock(4, 64, 0, composterLevel(t, "0"))
	if name, _ := servertest.StatePropsOf(t, srv, 4, 64, 0); name != "minecraft:composter" {
		t.Fatalf("the composter stands as %s", name)
	}
	if !srv.PlaceContainer(4, 63, 0, "minecraft:hopper") {
		t.Fatal("could not place the hopper under the composter")
	}
	if got := srv.InventoryAt(4, 64, 0); got != nil {
		t.Fatalf("the composter carries a block entity: %+v", got)
	}
	srv.StopTick()
	srv.SetLoadedBlock(4, 64, 0, composterLevel(t, "8"))

	// The composter's one bone meal drains, and its level falls to none.
	servertest.TickBlockEntitiesByHand(srv, 1)
	if hopper := srv.InventoryAt(4, 63, 0); servertest.NothingIn(hopper) ||
		!hopper[0].Equal(servertest.ItemStack(t, "minecraft:bone_meal", 1)) {
		t.Errorf("the hopper holds %+v, want the composter's one bone meal", hopper)
	}
	if _, props := servertest.StatePropsOf(t, srv, 4, 64, 0); props["level"] != "0" {
		t.Errorf("the drained composter's level is %v, want 0", props["level"])
	}
	if got := srv.HopperCooldownAt(4, 63, 0); got != 8 {
		t.Errorf("the hopper's cooldown after the drain is %d, want 8", got)
	}

	// The next move, the cooldown spent, finds the empty composter done.
	servertest.TickBlockEntitiesByHand(srv, 8)
	if hopper := srv.InventoryAt(4, 63, 0); !hopper[0].Equal(servertest.ItemStack(t, "minecraft:bone_meal", 1)) {
		t.Errorf("the hopper holds %+v, want still the one bone meal", hopper)
	}
	if _, props := servertest.StatePropsOf(t, srv, 4, 64, 0); props["level"] != "0" {
		t.Errorf("the drained composter's level is %v, want it still 0", props["level"])
	}
}

// TestAHopperFeedsTheComposterBelowIt checks the input container: a hopper
// facing down into a composter below level 7 posts one compostable through
// its top, the level rising by the roll a cake always wins and the fill
// event playing with the rose that matches. A composter at level 7, full
// and waiting for its ready tick, takes nothing and the hopper keeps its
// cake.
func TestAHopperFeedsTheComposterBelowIt(t *testing.T) {
	t.Parallel()
	srv, w := servertest.OpenWindowed(t, servertest.BlockTestConfig())

	srv.SetLoadedBlock(4, 63, 0, composterLevel(t, "0"))
	if !srv.PlaceContainer(4, 64, 0, "minecraft:hopper") {
		t.Fatal("could not place the hopper over the composter")
	}
	if _, props := servertest.StatePropsOf(t, srv, 4, 64, 0); props["facing"] != "down" {
		t.Fatalf("the hopper faces %v, want it facing down into the composter", props["facing"])
	}
	srv.StopTick()

	// Two cakes, whose composting chance is the certainty the level rises.
	if !srv.SetInventoryAt(4, 64, 0, 0, servertest.ItemStack(t, "minecraft:cake", 2)) {
		t.Fatal("the hopper has no block entity to fill")
	}
	servertest.TickBlockEntitiesByHand(srv, 1)
	if hopper := srv.InventoryAt(4, 64, 0); servertest.NothingIn(hopper) || !hopper[0].Equal(servertest.ItemStack(t, "minecraft:cake", 1)) {
		t.Errorf("the hopper holds %+v, want one cake left of its two", hopper)
	}
	if _, props := servertest.StatePropsOf(t, srv, 4, 63, 0); props["level"] != "1" {
		t.Errorf("the fed composter's level is %v, want it rose to 1", props["level"])
	}
	w.SettleAndCollect(func(id int32, data []byte) { w.Record(id, data) })
	if !slices.Contains(w.Events, rules.LevelEventComposterFill) {
		t.Errorf("the feed played no composter fill event; events were %v", w.Events)
	}

	// A composter that fills to level 7 takes nothing more: the cake
	// waits in the hopper for the ready tick's bone meal.
	srv.SetLoadedBlock(4, 63, 0, composterLevel(t, "7"))
	servertest.TickBlockEntitiesByHand(srv, 9)
	if hopper := srv.InventoryAt(4, 64, 0); servertest.NothingIn(hopper) || !hopper[0].Equal(servertest.ItemStack(t, "minecraft:cake", 1)) {
		t.Errorf("the hopper holds %+v, want the cake kept from the full composter", hopper)
	}
	if _, props := servertest.StatePropsOf(t, srv, 4, 63, 0); props["level"] != "7" {
		t.Errorf("the full composter's level is %v, want it still 7", props["level"])
	}
}

// TestAHopperMovesBothWaysThroughADecoratedPot checks the pot's one slot: the
// hopper under a pot pulls what it holds, one item of the stack per move,
// and the hopper over it posts into it, the slot's stack growing by each.
func TestAHopperMovesBothWaysThroughADecoratedPot(t *testing.T) {
	t.Parallel()
	srv, _ := servertest.OpenWindowed(t, servertest.BlockTestConfig())

	// The pot the hopper below drains.
	if !srv.PlaceContainer(4, 64, 0, "minecraft:decorated_pot") {
		t.Fatal("could not place the decorated pot")
	}
	if !srv.PlaceContainer(4, 63, 0, "minecraft:hopper") {
		t.Fatal("could not place the hopper under the pot")
	}
	// The pot the hopper above feeds.
	if !srv.PlaceContainer(6, 64, 0, "minecraft:decorated_pot") {
		t.Fatal("could not place the second decorated pot")
	}
	if !srv.PlaceContainer(6, 65, 0, "minecraft:hopper") {
		t.Fatal("could not place the hopper over the pot")
	}
	srv.StopTick()

	// Two dirt in the pot's one slot, two sticks over the other pot.
	if !srv.SetInventoryAt(4, 64, 0, 0, servertest.ItemStack(t, "minecraft:dirt", 2)) {
		t.Fatal("the pot has no slot to fill")
	}
	if !srv.SetInventoryAt(6, 65, 0, 0, servertest.ItemStack(t, "minecraft:stick", 2)) {
		t.Fatal("the hopper over the pot has no block entity to fill")
	}

	// One move one item: a dirt out of the pot, a stick into the other.
	servertest.TickBlockEntitiesByHand(srv, 1)
	if got := srv.InventoryAt(4, 64, 0); len(got) != 1 || !got[0].Equal(servertest.ItemStack(t, "minecraft:dirt", 1)) {
		t.Errorf("the drained pot holds %+v, want one dirt left", got)
	}
	if hopper := srv.InventoryAt(4, 63, 0); servertest.NothingIn(hopper) || !hopper[0].Equal(servertest.ItemStack(t, "minecraft:dirt", 1)) {
		t.Errorf("the hopper under the pot holds %+v, want the one dirt taken", hopper)
	}
	if got := srv.InventoryAt(6, 64, 0); len(got) != 1 || !got[0].Equal(servertest.ItemStack(t, "minecraft:stick", 1)) {
		t.Errorf("the fed pot holds %+v, want the one stick it was posted", got)
	}
	if hopper := srv.InventoryAt(6, 65, 0); servertest.NothingIn(hopper) || !hopper[0].Equal(servertest.ItemStack(t, "minecraft:stick", 1)) {
		t.Errorf("the hopper over the pot holds %+v, want one stick left of its two", hopper)
	}

	// The next moves, the cooldowns spent: the second dirt follows out of
	// the pot and the second stick lands on the first.
	servertest.TickBlockEntitiesByHand(srv, 8)
	if got := srv.InventoryAt(4, 64, 0); len(got) != 1 || !got[0].Empty() {
		t.Errorf("the drained pot holds %+v, want its last dirt gone", got)
	}
	if hopper := srv.InventoryAt(4, 63, 0); servertest.NothingIn(hopper) || !hopper[0].Equal(servertest.ItemStack(t, "minecraft:dirt", 2)) {
		t.Errorf("the hopper under the pot holds %+v, want both dirt taken", hopper)
	}
	if got := srv.InventoryAt(6, 64, 0); len(got) != 1 || !got[0].Equal(servertest.ItemStack(t, "minecraft:stick", 2)) {
		t.Errorf("the fed pot holds %+v, want its slot holding both sticks", got)
	}
}

// TestAHopperMovesBothWaysThroughAShelf checks the shelf's three slots the
// same way: the hopper under one takes from the first slot with something
// in it, and the hopper over another fills the first empty one.
func TestAHopperMovesBothWaysThroughAShelf(t *testing.T) {
	t.Parallel()
	srv, _ := servertest.OpenWindowed(t, servertest.BlockTestConfig())

	if !srv.PlaceContainer(4, 64, 0, "minecraft:oak_shelf") {
		t.Fatal("could not place the shelf")
	}
	if !srv.PlaceContainer(4, 63, 0, "minecraft:hopper") {
		t.Fatal("could not place the hopper under the shelf")
	}
	if !srv.PlaceContainer(6, 64, 0, "minecraft:oak_shelf") {
		t.Fatal("could not place the second shelf")
	}
	if !srv.PlaceContainer(6, 65, 0, "minecraft:hopper") {
		t.Fatal("could not place the hopper over the shelf")
	}
	srv.StopTick()

	// Dirt in the shelf's last slot, sticks to feed the other with.
	if !srv.SetInventoryAt(4, 64, 0, 2, servertest.ItemStack(t, "minecraft:dirt", 1)) {
		t.Fatal("the shelf has no third slot to fill")
	}
	if !srv.SetInventoryAt(6, 65, 0, 0, servertest.ItemStack(t, "minecraft:stick", 2)) {
		t.Fatal("the hopper over the shelf has no block entity to fill")
	}

	servertest.TickBlockEntitiesByHand(srv, 1)
	if got := srv.InventoryAt(4, 64, 0); len(got) != 3 || !got[2].Empty() {
		t.Errorf("the drained shelf holds %+v, want its third slot emptied", got)
	}
	if hopper := srv.InventoryAt(4, 63, 0); servertest.NothingIn(hopper) || !hopper[0].Equal(servertest.ItemStack(t, "minecraft:dirt", 1)) {
		t.Errorf("the hopper under the shelf holds %+v, want the dirt taken", hopper)
	}
	if got := srv.InventoryAt(6, 64, 0); len(got) != 3 || !got[0].Equal(servertest.ItemStack(t, "minecraft:stick", 1)) {
		t.Errorf("the fed shelf holds %+v, want a stick in its first slot", got)
	}

	// The next moves, the cooldowns spent: the stick lands on the first
	// too, the shelf stacking its slots as any container does.
	servertest.TickBlockEntitiesByHand(srv, 8)
	if got := srv.InventoryAt(6, 64, 0); len(got) != 3 || !got[0].Equal(servertest.ItemStack(t, "minecraft:stick", 2)) {
		t.Errorf("the fed shelf holds %+v, want both sticks in its first slot", got)
	}
	if got := srv.InventoryAt(4, 64, 0); !servertest.NothingIn(got) {
		t.Errorf("the drained shelf holds %+v, want it empty", got)
	}
}

// TestAComparatorReadsADecoratedPot checks the pot's analog output: the
// comparator beside it reads the fullness of its one slot, fifteen for a full
// stack and one for a single item.
// vanilla: DecoratedPotBlock.getAnalogOutputSignal
func TestAComparatorReadsADecoratedPot(t *testing.T) {
	t.Parallel()
	srv, c := servertest.AttachActor(t)

	servertest.Setblock(t, c, 4, 64, 0, "minecraft:decorated_pot")
	if !srv.SetInventoryAt(4, 64, 0, 0, servertest.ItemStack(t, "minecraft:dirt", 64)) {
		t.Fatal("the pot has no block entity to fill")
	}
	servertest.Setblock(t, c, 4, 64, 3, "minecraft:decorated_pot")
	if !srv.SetInventoryAt(4, 64, 3, 0, servertest.ItemStack(t, "minecraft:dirt", 1)) {
		t.Fatal("the second pot has no block entity to fill")
	}
	wire, _ := registry.FindState("minecraft:redstone_wire", nil)
	if !srv.SetBlock(6, 64, 0, wire) || !srv.SetBlock(6, 64, 3, wire) {
		t.Fatal("the wires' chunk is not loaded")
	}
	servertest.Setblock(t, c, 5, 64, 0, "minecraft:comparator[facing=west,mode=compare,powered=false]")
	servertest.Setblock(t, c, 5, 64, 1, "stone")
	servertest.Setblock(t, c, 5, 64, 3, "minecraft:comparator[facing=west,mode=compare,powered=false]")
	servertest.Setblock(t, c, 5, 64, 4, "stone")

	// A full stack in the one slot reads floor(1/1*14)+1 = 15; one item of
	// a stack of 64 reads floor(1/64*14)+1 = 1.
	servertest.ExpectWirePower(t, srv, 6, 64, 0, "15")
	servertest.ExpectWirePower(t, srv, 6, 64, 3, "1")
}
