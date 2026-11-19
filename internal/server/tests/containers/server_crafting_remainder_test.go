package containers

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
)

// What a craft leaves behind: an ingredient whose item carries a crafting
// remainder comes back empty when the take spends it, a milk bucket as an
// empty bucket, a honey bottle as a glass bottle. Each remainder is set
// back into the grid cell it came from, into the player's things where the
// cell still holds its ingredient, and on the ground where neither has
// room, on every take: a click, a shift-click, a Q throw and the Ctrl+Q
// repeat. vanilla: ResultSlot.onTake, Item.getCraftingRemainder

// TestCraftingACakeLeavesAnEmptyBucketPerMilkBucket fills a crafting
// table with the cake recipe and clicks the cake out: the three buckets
// of milk are spent, and an empty bucket sits in each of their cells -
// the crafting remainder, not the nothing their consumption leaves.
func TestCraftingACakeLeavesAnEmptyBucketPerMilkBucket(t *testing.T) {
	t.Parallel()
	_, w := servertest.OpenWindowed(t, servertest.BlockTestConfig())

	w.SetCreativeSlot(36, servertest.ItemStack(t, "minecraft:crafting_table", 1))
	w.PlaceBlock(4, 63, 0, protocol.FaceTop)
	w.ExpectBlockAck()
	w.UseBlock(4, 64, 0)
	w.ExpectContainerOpen(protocol.MenuCrafting, false, 0, 0, 0)

	// The cake's cubes: three milk buckets over sugar, an egg, sugar,
	// over three wheat, one item per cell out of the hotbar so every
	// ingredient is spent whole.
	w.SetCreativeSlot(36, servertest.ItemStack(t, "minecraft:milk_bucket", 1))
	w.SetCreativeSlot(37, servertest.ItemStack(t, "minecraft:milk_bucket", 1))
	w.SetCreativeSlot(38, servertest.ItemStack(t, "minecraft:milk_bucket", 1))
	w.SetCreativeSlot(39, servertest.ItemStack(t, "minecraft:sugar", 2))
	w.SetCreativeSlot(40, servertest.ItemStack(t, "minecraft:egg", 1))
	w.SetCreativeSlot(41, servertest.ItemStack(t, "minecraft:wheat", 3))
	w.PutInGrid(37, 1)
	w.PutInGrid(38, 2)
	w.PutInGrid(39, 3)
	w.PutInGrid(40, 4, 6)
	w.PutInGrid(41, 5)
	w.PutInGrid(42, 7, 8, 9)

	w.Click(0, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{
		0: {},
		1: servertest.ItemStack(t, "minecraft:bucket", 1),
		2: servertest.ItemStack(t, "minecraft:bucket", 1),
		3: servertest.ItemStack(t, "minecraft:bucket", 1),
		4: {}, 5: {}, 6: {}, 7: {}, 8: {}, 9: {},
	}, servertest.ItemStack(t, "minecraft:cake", 1))
}

// TestShiftCraftingAHoneyBlockLeavesTheBottlesBehind fills the player's
// own 2x2 with honey bottles and shift-clicks the honey block they are
// worth: the craft spends the four bottles and sets a glass bottle back
// into each cell, and the grid of bottles answers with nothing.
func TestShiftCraftingAHoneyBlockLeavesTheBottlesBehind(t *testing.T) {
	t.Parallel()
	srv, w := servertest.OpenWindowed(t, servertest.BlockTestConfig())

	w.SetCreativeSlot(36, servertest.ItemStack(t, "minecraft:honey_bottle", 4))
	w.PutInGrid(36, 1, 2, 3, 4)

	w.Click(0, 0, protocol.ClickModeQuickMove)
	w.ExpectClickResult(map[int16]protocol.ItemStack{
		0:  {},
		1:  servertest.ItemStack(t, "minecraft:glass_bottle", 1),
		2:  servertest.ItemStack(t, "minecraft:glass_bottle", 1),
		3:  servertest.ItemStack(t, "minecraft:glass_bottle", 1),
		4:  servertest.ItemStack(t, "minecraft:glass_bottle", 1),
		44: servertest.ItemStack(t, "minecraft:honey_block", 1),
	}, protocol.ItemStack{})
	for slot := 1; slot <= 4; slot++ {
		if got := srv.InventorySlot("Testificate", slot); !got.Equal(servertest.ItemStack(t, "minecraft:glass_bottle", 1)) {
			t.Errorf("server's grid cell %d = %+v, want a glass bottle", slot, got)
		}
	}
}

// TestARemainderFromAStackedCellFallsIntoTheInventory loads each cell of
// the 2x2 with two honey bottles, so one craft leaves a bottle in each
// cell: a cell that still holds its ingredient cannot take the glass
// bottle the spent one leaves, and it goes to the player's things, the
// hotbar's first free slot holding all four.
func TestARemainderFromAStackedCellFallsIntoTheInventory(t *testing.T) {
	t.Parallel()
	srv, w := servertest.OpenWindowed(t, servertest.BlockTestConfig())

	w.SetCreativeSlot(36, servertest.ItemStack(t, "minecraft:honey_bottle", 8))
	w.PutInGrid(36, 1, 1, 2, 2, 3, 3, 4, 4)

	// The cells hold a bottle each, so the grid is worth a honey block
	// still.
	w.Click(0, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{
		0:  servertest.ItemStack(t, "minecraft:honey_block", 1),
		1:  servertest.ItemStack(t, "minecraft:honey_bottle", 1),
		2:  servertest.ItemStack(t, "minecraft:honey_bottle", 1),
		3:  servertest.ItemStack(t, "minecraft:honey_bottle", 1),
		4:  servertest.ItemStack(t, "minecraft:honey_bottle", 1),
		36: servertest.ItemStack(t, "minecraft:glass_bottle", 4),
	}, servertest.ItemStack(t, "minecraft:honey_block", 1))
	if got := srv.InventorySlot("Testificate", 36); !got.Equal(servertest.ItemStack(t, "minecraft:glass_bottle", 4)) {
		t.Errorf("server's slot 36 = %+v, want the four glass bottles", got)
	}
}

// TestThrownCraftsDropTheRemaindersOnTheGround fills the player's things
// and throws crafts out of the 2x2 with Q: with two honey bottles in
// every cell and nowhere a glass bottle fits, the first craft leaves one
// bottle per cell and drops each glass bottle on the ground, and the
// Ctrl+Q repeat empties every cell - whose remainders now have their
// cells back - and ends with the grid of glass bottles worth nothing.
func TestThrownCraftsDropTheRemaindersOnTheGround(t *testing.T) {
	t.Parallel()
	_, w := servertest.OpenWindowed(t, servertest.BlockTestConfig())

	// A full inventory with one hole kept for the honey bottles that
	// fill the grid, shut behind them once they are laid out.
	for slot := int16(9); slot <= 45; slot++ {
		w.SetCreativeSlot(slot, servertest.ItemStack(t, "minecraft:dirt", 64))
	}
	w.SetCreativeSlot(36, servertest.ItemStack(t, "minecraft:honey_bottle", 8))
	w.PutInGrid(36, 1, 1, 2, 2, 3, 3, 4, 4)
	w.SetCreativeSlot(36, servertest.ItemStack(t, "minecraft:dirt", 64))

	// The plain Q throws one craft: each glass bottle drops, no cell
	// having room for it, and the thrown honey block lands after them.
	// An empty off hand is never the place a remainder settles.
	w.Click(0, 0, protocol.ClickModeThrow)
	w.ExpectClickResult(map[int16]protocol.ItemStack{
		0: servertest.ItemStack(t, "minecraft:honey_block", 1),
		1: servertest.ItemStack(t, "minecraft:honey_bottle", 1),
		2: servertest.ItemStack(t, "minecraft:honey_bottle", 1),
		3: servertest.ItemStack(t, "minecraft:honey_bottle", 1),
		4: servertest.ItemStack(t, "minecraft:honey_bottle", 1),
	}, protocol.ItemStack{})
	w.AwaitDrops(5)
	w.ExpectDrop(servertest.ItemStack(t, "minecraft:glass_bottle", 1))
	w.ExpectDrop(servertest.ItemStack(t, "minecraft:glass_bottle", 1))
	w.ExpectDrop(servertest.ItemStack(t, "minecraft:glass_bottle", 1))
	w.ExpectDrop(servertest.ItemStack(t, "minecraft:glass_bottle", 1))
	w.ExpectDrop(servertest.ItemStack(t, "minecraft:honey_block", 1))

	// The Ctrl+Q repeats while the grid keeps its worth: the second
	// craft spends the last bottle of every cell, puts its glass
	// bottle back in each, and throws the second honey block.
	w.Click(0, 1, protocol.ClickModeThrow)
	w.ExpectClickResult(map[int16]protocol.ItemStack{
		0: {},
		1: servertest.ItemStack(t, "minecraft:glass_bottle", 1),
		2: servertest.ItemStack(t, "minecraft:glass_bottle", 1),
		3: servertest.ItemStack(t, "minecraft:glass_bottle", 1),
		4: servertest.ItemStack(t, "minecraft:glass_bottle", 1),
	}, protocol.ItemStack{})
	w.AwaitDrops(1)
	w.ExpectDrop(servertest.ItemStack(t, "minecraft:honey_block", 1))
}
