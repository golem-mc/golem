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
