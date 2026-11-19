package redstone

import (
	"slices"
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/world"
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

// TestAHopperBelowAComposterThatIsNotReadyTakesNothing checks the composter
// that has no bone meal to give: below level 8 nothing comes out of the
// bottom face, however long the hopper waits.
func TestAHopperBelowAComposterThatIsNotReadyTakesNothing(t *testing.T) {
	t.Parallel()
	srv, _ := servertest.OpenWindowed(t, servertest.BlockTestConfig())

	srv.SetLoadedBlock(4, 64, 0, composterLevel(t, "7"))
	if !srv.PlaceContainer(4, 63, 0, "minecraft:hopper") {
		t.Fatal("could not place the hopper under the composter")
	}
	srv.StopTick()

	servertest.TickBlockEntitiesByHand(srv, 10)
	if hopper := srv.InventoryAt(4, 63, 0); !servertest.NothingIn(hopper) {
		t.Errorf("the hopper holds %+v, want nothing taken from a composter at level 7", hopper)
	}
	if _, props := servertest.StatePropsOf(t, srv, 4, 64, 0); props["level"] != "7" {
		t.Errorf("the composter's level is %v, want it still 7", props["level"])
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

// TestAHopperMovesBothWaysThroughACopperChest checks the copper chest, whose
// block entity is the chest's: the hopper under it pulls its slots, and the
// one over it posts into the first of them.
func TestAHopperMovesBothWaysThroughACopperChest(t *testing.T) {
	t.Parallel()
	srv, _ := servertest.OpenWindowed(t, servertest.BlockTestConfig())

	if !srv.PlaceContainer(4, 64, 0, "minecraft:copper_chest") {
		t.Fatal("could not place the copper chest")
	}
	if !srv.PlaceContainer(4, 63, 0, "minecraft:hopper") {
		t.Fatal("could not place the hopper under the copper chest")
	}
	if !srv.PlaceContainer(6, 64, 0, "minecraft:copper_chest") {
		t.Fatal("could not place the second copper chest")
	}
	if !srv.PlaceContainer(6, 65, 0, "minecraft:hopper") {
		t.Fatal("could not place the hopper over the copper chest")
	}
	srv.StopTick()

	if !srv.SetInventoryAt(4, 64, 0, 0, servertest.ItemStack(t, "minecraft:dirt", 2)) {
		t.Fatal("the copper chest has no slot to fill")
	}
	if !srv.SetInventoryAt(6, 65, 0, 0, servertest.ItemStack(t, "minecraft:stick", 2)) {
		t.Fatal("the hopper over the copper chest has no block entity to fill")
	}

	servertest.TickBlockEntitiesByHand(srv, 1)
	if got := srv.InventoryAt(4, 64, 0); len(got) < 1 || !got[0].Equal(servertest.ItemStack(t, "minecraft:dirt", 1)) {
		t.Errorf("the drained copper chest holds %+v, want one dirt left in its first slot", got)
	}
	if hopper := srv.InventoryAt(4, 63, 0); servertest.NothingIn(hopper) || !hopper[0].Equal(servertest.ItemStack(t, "minecraft:dirt", 1)) {
		t.Errorf("the hopper under the copper chest holds %+v, want the one dirt taken", hopper)
	}
	if got := srv.InventoryAt(6, 64, 0); len(got) < 1 || !got[0].Equal(servertest.ItemStack(t, "minecraft:stick", 1)) {
		t.Errorf("the fed copper chest holds %+v, want a stick in its first slot", got)
	}

	servertest.TickBlockEntitiesByHand(srv, 8)
	if got := srv.InventoryAt(4, 64, 0); len(got) < 1 || !got[0].Empty() {
		t.Errorf("the drained copper chest holds %+v, want its last dirt gone", got)
	}
	if got := srv.InventoryAt(6, 64, 0); len(got) < 1 || !got[0].Equal(servertest.ItemStack(t, "minecraft:stick", 2)) {
		t.Errorf("the fed copper chest holds %+v, want both sticks in its first slot", got)
	}
}

// TestAHopperMovesThroughAPotInTheNetherToo checks the moves reach the other
// dimensions: a hopper over a decorated pot in the nether posts into it
// the same as in the overworld, the pot's block entity being each
// dimension's own.
func TestAHopperMovesThroughAPotInTheNetherToo(t *testing.T) {
	t.Parallel()
	srv, w := servertest.OpenWindowed(t, servertest.BlockTestConfig())
	servertest.CarryIntoNether(t, srv, w.Client, "Testificate", 8, 8)

	if !srv.PlaceContainerInDimension(world.NetherName, 8, 40, 8, "minecraft:decorated_pot") {
		t.Fatal("could not place the nether decorated pot")
	}
	if !srv.PlaceContainerInDimension(world.NetherName, 8, 41, 8, "minecraft:hopper") {
		t.Fatal("could not place the nether hopper")
	}
	srv.StopTick()
	if !srv.SetInventoryInDimension(world.NetherName, 8, 41, 8, 0, servertest.ItemStack(t, "minecraft:dirt", 1)) {
		t.Fatal("the nether hopper has no block entity to fill")
	}

	servertest.TickBlockEntitiesByHand(srv, 1)
	if got := srv.InventoryInDimension(world.NetherName, 8, 40, 8); len(got) != 1 ||
		!got[0].Equal(servertest.ItemStack(t, "minecraft:dirt", 1)) {
		t.Errorf("the fed nether pot holds %+v, want the one dirt posted into it", got)
	}
	if got := srv.InventoryInDimension(world.NetherName, 8, 41, 8); len(got) != 5 || !servertest.NothingIn(got) {
		t.Errorf("the nether hopper holds %+v, want its dirt spent", got)
	}
}

// TestAPotAndComposterKeepTheirStateAcrossARestart checks the composter
// feed's level and the pot's item written where the region file keeps them:
// a restart reads both back the same.
func TestAPotAndComposterKeepTheirStateAcrossARestart(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	cfg.LevelName = t.TempDir()

	srv, w := servertest.OpenWindowedOps(t, cfg)
	servertest.Setblock(t, w.Client, 4, 63, 0, "minecraft:composter[level=0]")
	if !srv.PlaceContainer(4, 64, 0, "minecraft:hopper") {
		t.Fatal("could not place the hopper over the composter")
	}
	if !srv.PlaceContainer(6, 64, 0, "minecraft:decorated_pot") {
		t.Fatal("could not place the decorated pot")
	}
	if !srv.PlaceContainer(6, 65, 0, "minecraft:hopper") {
		t.Fatal("could not place the hopper over the pot")
	}
	srv.StopTick()
	if !srv.SetInventoryAt(4, 64, 0, 0, servertest.ItemStack(t, "minecraft:cake", 1)) {
		t.Fatal("the hopper has no block entity to fill")
	}
	if !srv.SetInventoryAt(6, 65, 0, 0, servertest.ItemStack(t, "minecraft:dirt", 1)) {
		t.Fatal("the hopper over the pot has no block entity to fill")
	}
	servertest.TickBlockEntitiesByHand(srv, 1)
	w.Disconnect()
	srv.Shutdown()

	back, w2 := servertest.OpenWindowedOps(t, cfg)
	defer w2.Disconnect()
	if _, props := servertest.StatePropsOf(t, back, 4, 63, 0); props["level"] != "1" {
		t.Errorf("the fed composter's level after the restart is %v, want 1", props["level"])
	}
	if got := back.InventoryAt(6, 64, 0); len(got) != 1 || !got[0].Equal(servertest.ItemStack(t, "minecraft:dirt", 1)) {
		t.Errorf("the fed pot holds %+v after the restart, want its dirt kept", got)
	}
}

// TestItemsAboveTheWindowlessContainersAreNotSucked keeps the mouth: the
// composter and the copper chest cover the hopper's, so an item entity
// lying in their cell stays lying there - the composter being a container
// at every level, the chest having no slots to drain, there being no
// block entity beneath it.
func TestItemsAboveTheWindowlessContainersAreNotSucked(t *testing.T) {
	t.Parallel()
	srv, _ := servertest.OpenWindowed(t, servertest.BlockTestConfig())

	if !srv.PlaceContainer(4, 63, 0, "minecraft:hopper") {
		t.Fatal("could not place the hopper under the composter")
	}
	srv.SetLoadedBlock(4, 64, 0, composterLevel(t, "3"))
	if !srv.PlaceContainer(6, 63, 0, "minecraft:hopper") {
		t.Fatal("could not place the hopper under the copper chest")
	}
	srv.SetLoadedBlock(6, 64, 0, registry.BlockStates["minecraft:copper_chest"])
	srv.StopTick()

	for _, x := range []int32{4, 6} {
		if srv.SpawnItemEntity(float64(x)+0.5, 64.0, 0.5, "minecraft:stick", 3) == 0 {
			t.Fatal("the item entity did not spawn")
		}
	}

	servertest.TickBlockEntitiesByHand(srv, 1)
	for _, x := range []int32{4, 6} {
		if got := srv.InventoryAt(x, 63, 0); !servertest.NothingIn(got) {
			t.Errorf("the hopper at x=%d holds %+v, want nothing taken in", x, got)
		}
	}
	if items := srv.ItemsInDimension(world.DimensionName); len(items) != 2 {
		t.Errorf("the items above hold %d entities, want both left lying", len(items))
	}
	if _, props := servertest.StatePropsOf(t, srv, 4, 64, 0); props["level"] != "3" {
		t.Errorf("the composter's level is %v, want it unchanged at 3", props["level"])
	}
}

// TestAPotWithALootTableKeepsWhatAHopperFeedsIt checks the pot's save: a
// transfer clears the loot table, so the item a hopper posted survives the
// restart rather than dropping with a table the save would otherwise write
// ahead of it. The table names none that exists, so it rolls nothing into the
// pot's one slot and leaves it free for the dirt.
// vanilla: DecoratedPotBlockEntity.unpackLootTable, saveAdditional
func TestAPotWithALootTableKeepsWhatAHopperFeedsIt(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	cfg.LevelName = t.TempDir()

	srv, w := servertest.OpenWindowedOps(t, cfg)
	servertest.Setblock(t, w.Client, 4, 64, 0,
		`minecraft:decorated_pot{LootTable:"minecraft:chests/no_such_table"}`)
	if !srv.PlaceContainer(4, 65, 0, "minecraft:hopper") {
		t.Fatal("could not place the hopper over the pot")
	}
	srv.StopTick()
	if !srv.SetInventoryAt(4, 65, 0, 0, servertest.ItemStack(t, "minecraft:dirt", 1)) {
		t.Fatal("the hopper has no block entity to fill")
	}
	servertest.TickBlockEntitiesByHand(srv, 1)
	if got := srv.InventoryAt(4, 64, 0); len(got) != 1 || !got[0].Equal(servertest.ItemStack(t, "minecraft:dirt", 1)) {
		t.Fatalf("the fed pot holds %+v, want the dirt the hopper posted", got)
	}
	w.Disconnect()
	srv.Shutdown()

	back, w2 := servertest.OpenWindowedOps(t, cfg)
	defer w2.Disconnect()
	if got := back.InventoryAt(4, 64, 0); len(got) != 1 || !got[0].Equal(servertest.ItemStack(t, "minecraft:dirt", 1)) {
		t.Errorf("the fed pot holds %+v after the restart, want its dirt kept", got)
	}
	tag, ok := back.BlockEntityTagAt(4, 64, 0)
	if !ok {
		t.Fatal("the pot has no block entity tag after the restart")
	}
	if table, ok := tag["LootTable"]; ok {
		t.Errorf("the fed pot still saves its loot table (%v) beside the item", table)
	}
	if _, ok := tag["item"]; !ok {
		t.Errorf("the fed pot saves no item: %v", tag)
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

// TestAComparatorReadsAShelfOnlyFromBehind checks the shelf's analog output:
// a bitmask of the occupied slots, read only through the face opposite the
// shelf's own and nothing from the front.
// vanilla: ShelfBlock.getAnalogOutputSignal
func TestAComparatorReadsAShelfOnlyFromBehind(t *testing.T) {
	t.Parallel()
	srv, c := servertest.AttachActor(t)

	servertest.Setblock(t, c, 4, 64, 0, "minecraft:oak_shelf[facing=north]")
	if !srv.SetInventoryAt(4, 64, 0, 0, servertest.ItemStack(t, "minecraft:dirt", 1)) ||
		!srv.SetInventoryAt(4, 64, 0, 2, servertest.ItemStack(t, "minecraft:dirt", 1)) {
		t.Fatal("the shelf has no slots to fill")
	}
	// The wire behind the back comparator, and one behind a front one.
	wire, _ := registry.FindState("minecraft:redstone_wire", nil)
	if !srv.SetBlock(4, 64, 2, wire) || !srv.SetBlock(4, 64, -2, wire) {
		t.Fatal("the wires' chunk is not loaded")
	}
	// The shelf faces north, so its back is south: the comparator at
	// (4,64,1) facing north reads it, the one at (4,64,-1) facing south
	// reads its front and gets nothing.
	servertest.Setblock(t, c, 4, 64, 1, "minecraft:comparator[facing=north,mode=compare,powered=false]")
	servertest.Setblock(t, c, 5, 64, 1, "stone")
	servertest.Setblock(t, c, 4, 64, -1, "minecraft:comparator[facing=south,mode=compare,powered=false]")
	servertest.Setblock(t, c, 5, 64, -1, "stone")

	// Slots 0 and 2 hold an item: 1 | 1<<2 = 5.
	servertest.ExpectWirePower(t, srv, 4, 64, 2, "5")
	servertest.ExpectWirePower(t, srv, 4, 64, -2, "0")
}

// TestADropperFeedsADecoratedPot checks the dropper's half of the move: its
// push goes into the pot through the same AddItem a hopper's does.
func TestADropperFeedsADecoratedPot(t *testing.T) {
	t.Parallel()
	srv, _ := servertest.OpenWindowed(t, servertest.BlockTestConfig())

	if !srv.PlaceContainer(4, 64, 0, "minecraft:decorated_pot") {
		t.Fatal("could not place the decorated pot")
	}
	if !srv.PlaceContainer(4, 64, -1, "minecraft:dropper") {
		t.Fatal("could not place the dropper")
	}
	srv.StopTick()
	if !srv.SetInventoryAt(4, 64, -1, 0, servertest.ItemStack(t, "minecraft:dirt", 1)) {
		t.Fatal("the dropper has no block entity to fill")
	}

	srv.Dispense(4, 64, -1, protocol.FaceSouth)
	if got := srv.InventoryAt(4, 64, 0); len(got) != 1 || !got[0].Equal(servertest.ItemStack(t, "minecraft:dirt", 1)) {
		t.Errorf("the pot holds %+v, want the one dirt the dropper pushed", got)
	}
}

// TestASideFacingHopperFeedsAShelf checks a hopper whose spout opens
// sideways: it feeds the shelf beside it the same as one facing down.
func TestASideFacingHopperFeedsAShelf(t *testing.T) {
	t.Parallel()
	srv, _ := servertest.OpenWindowed(t, servertest.BlockTestConfig())

	if !srv.PlaceContainer(4, 64, 0, "minecraft:oak_shelf") {
		t.Fatal("could not place the shelf")
	}
	hopper, _ := registry.FindState("minecraft:hopper", map[string]string{"facing": "east"})
	if !srv.SetBlock(3, 64, 0, hopper) ||
		!srv.EnsureBlockEntityInDimension(world.DimensionName, 3, 64, 0, world.KindHopper) {
		t.Fatal("could not place the side-facing hopper")
	}
	srv.StopTick()
	if !srv.SetInventoryAt(3, 64, 0, 0, servertest.ItemStack(t, "minecraft:stick", 2)) {
		t.Fatal("the hopper has no block entity to fill")
	}

	servertest.TickBlockEntitiesByHand(srv, 1)
	if got := srv.InventoryAt(4, 64, 0); len(got) != 3 || !got[0].Equal(servertest.ItemStack(t, "minecraft:stick", 1)) {
		t.Errorf("the shelf holds %+v, want a stick in its first slot", got)
	}
	if got := srv.InventoryAt(3, 64, 0); servertest.NothingIn(got) || !got[0].Equal(servertest.ItemStack(t, "minecraft:stick", 1)) {
		t.Errorf("the hopper holds %+v, want one stick left of its two", got)
	}
}

// TestAHopperMovesThroughACopperChestPair checks the double copper chest as
// one target of 54 slots: a hopper over a full half reaches the other.
func TestAHopperMovesThroughACopperChestPair(t *testing.T) {
	t.Parallel()
	srv, w := servertest.OpenWindowed(t, servertest.BlockTestConfig())

	servertest.PlaceCopperChestPair(t, w, "minecraft:copper_chest", "minecraft:copper_chest", 4, 64, 0)
	if !srv.PlaceContainer(4, 65, 0, "minecraft:hopper") {
		t.Fatal("could not place the hopper over the left half")
	}
	srv.StopTick()

	// The left half full: the hopper's push can only land in the right
	// half, which it reaches only while the pair is one target.
	dirt := servertest.ItemStack(t, "minecraft:dirt", 64)
	for slot := range 27 {
		if !srv.SetInventoryAt(4, 64, 0, slot, dirt) {
			t.Fatalf("the left half has no slot %d", slot)
		}
	}
	if !srv.SetInventoryAt(4, 65, 0, 0, servertest.ItemStack(t, "minecraft:stick", 1)) {
		t.Fatal("the hopper has no block entity to fill")
	}

	servertest.TickBlockEntitiesByHand(srv, 1)
	if got := srv.InventoryAt(5, 64, 0); len(got) < 1 || !got[0].Equal(servertest.ItemStack(t, "minecraft:stick", 1)) {
		t.Errorf("the right half holds %+v, want the stick the hopper pushed across the pair", got)
	}
	if got := srv.InventoryAt(4, 65, 0); !servertest.NothingIn(got) {
		t.Errorf("the hopper holds %+v, want its stick spent", got)
	}
}

// TestAPotAndShelfTellTheirItems checks the Block Entity Data a move
// broadcasts: a hopper filling a pot or a shelf tells the world its new
// contents, which is how a client renders the item.
func TestAPotAndShelfTellTheirItems(t *testing.T) {
	t.Parallel()
	srv, w := servertest.OpenWindowed(t, servertest.BlockTestConfig())

	if !srv.PlaceContainer(4, 64, 0, "minecraft:decorated_pot") ||
		!srv.PlaceContainer(4, 65, 0, "minecraft:hopper") {
		t.Fatal("could not place the pot and its hopper")
	}
	if !srv.PlaceContainer(6, 64, 0, "minecraft:oak_shelf") ||
		!srv.PlaceContainer(6, 65, 0, "minecraft:hopper") {
		t.Fatal("could not place the shelf and its hopper")
	}
	srv.StopTick()
	if !srv.SetInventoryAt(4, 65, 0, 0, servertest.ItemStack(t, "minecraft:dirt", 1)) ||
		!srv.SetInventoryAt(6, 65, 0, 0, servertest.ItemStack(t, "minecraft:stick", 1)) {
		t.Fatal("the hoppers have no block entity to fill")
	}

	told := map[[3]int32]bool{}
	servertest.TickBlockEntitiesByHand(srv, 1)
	w.SettleAndCollect(func(id int32, data []byte) {
		if id != protocol.IDPlayBlockEntityData {
			return
		}
		x, y, z := protocol.NewReader(data).ReadPosition()
		told[[3]int32{x, y, z}] = true
	})
	if !told[[3]int32{4, 64, 0}] {
		t.Errorf("the filled pot told nobody its item; block entity data came for %v", told)
	}
	if !told[[3]int32{6, 64, 0}] {
		t.Errorf("the filled shelf told nobody its item; block entity data came for %v", told)
	}
}
