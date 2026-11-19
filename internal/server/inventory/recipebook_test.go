package inventory

import (
	"slices"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/registry"
)

// craftingTable is the display of minecraft:crafting_table, the shaped
// two-by-two of the planks tag: the recipe the tests place.
func craftingTableDisplay(t *testing.T) Recipe {
	t.Helper()
	for i := range registry.CraftingDisplays {
		if registry.CraftingDisplays[i].Name == "minecraft:crafting_table" {
			return Recipe{
				Shaped:  registry.CraftingDisplays[i].Shaped,
				Width:   registry.CraftingDisplays[i].Width,
				Height:  registry.CraftingDisplays[i].Height,
				Squares: registry.CraftingDisplays[i].Items,
			}
		}
	}
	t.Fatal("no crafting_table in the display table")
	return Recipe{}
}

// TestPlaceRecipeFillsAPlanksGridFromTheInventory checks the placement of
// the crafting table recipe in the player's own 2x2: the four planks leave
// the inventory for the grid, one a square, and every cell the placement
// touched is announced with the result slot behind them.
func TestPlaceRecipeFillsAPlanksGridFromTheInventory(t *testing.T) {
	oakPlanks := protocol.ItemStack{ItemID: regItemID(t, "minecraft:oak_planks"), Count: 5}
	inv := &player.Inventory{}
	inv.Set(player.HotbarSlot, oakPlanks)
	view := View{Kind: KindPlayer}
	outcome := Outcome{}

	if !PlaceRecipe(view, inv, craftingTableDisplay(t), false, false, &outcome) {
		t.Fatal("a placement the inventory could pay was reported unplaceable")
	}
	for cell := range playerGridSlots {
		want := protocol.ItemStack{ItemID: oakPlanks.ItemID, Count: 1}
		if got := inv.Slots[playerCraftingGrid+cell]; !got.Equal(want) {
			t.Errorf("grid cell %d = %+v, want one plank", cell, got)
		}
	}
	if got := inv.Slots[player.HotbarSlot]; got.Count != 1 {
		t.Errorf("hotbar slot = %+v, want the plank left of five", got)
	}
	want := []int32{0, playerCraftingGrid, playerCraftingGrid + 1,
		playerCraftingGrid + 2, playerCraftingGrid + 3, player.HotbarSlot}
	for _, slot := range want {
		if !slices.Contains(outcome.Announce, slot) {
			t.Errorf("the placement did not announce window slot %d", slot)
		}
	}
}

// TestPlaceRecipeCountsTheGridAmongTheItems checks the placement counts the
// crafting grid as well: two planks by the player's hand and two in the
// grid make the craft, the grid handed back to the inventory first and
// refilled as one craft of one a square.
func TestPlaceRecipeCountsTheGridAmongTheItems(t *testing.T) {
	inv := &player.Inventory{}
	planks := regItemID(t, "minecraft:oak_planks")
	inv.Set(player.HotbarSlot, protocol.ItemStack{ItemID: planks, Count: 2})
	inv.Slots[playerCraftingGrid] = protocol.ItemStack{ItemID: planks, Count: 2}
	view := View{Kind: KindPlayer}
	outcome := Outcome{}

	if !PlaceRecipe(view, inv, craftingTableDisplay(t), false, false, &outcome) {
		t.Fatal("a placement the grid could top up was reported unplaceable")
	}
	for cell := range playerGridSlots {
		if got := inv.Slots[playerCraftingGrid+cell]; got.Count != 1 {
			t.Errorf("grid cell %d = %+v, want one plank", cell, got)
		}
	}
	// The grid's two planks were handed back onto the hotbar's two before
	// the refill drew four out again.
	if got := inv.Slots[player.HotbarSlot]; !got.Empty() {
		t.Errorf("hotbar slot = %+v, want it emptied by the craft", got)
	}
}

// TestPlaceRecipeWithoutTheItemsClearsTheGrid checks the placement that
// cannot be made: the grid is given back to the inventory, the emptied grid
// and the result slot behind it are announced, and the caller learns to
// answer with the ghost recipe.
func TestPlaceRecipeWithoutTheItemsClearsTheGrid(t *testing.T) {
	inv := &player.Inventory{}
	planks := regItemID(t, "minecraft:oak_planks")
	inv.Set(player.HotbarSlot, protocol.ItemStack{ItemID: planks, Count: 2})
	inv.Slots[playerCraftingGrid] = protocol.ItemStack{ItemID: planks, Count: 1}
	view := View{Kind: KindPlayer}
	outcome := Outcome{}

	if PlaceRecipe(view, inv, craftingTableDisplay(t), false, false, &outcome) {
		t.Fatal("a placement a plank short of was reported placeable")
	}
	if got := inv.Slots[playerCraftingGrid]; !got.Empty() {
		t.Errorf("grid cell = %+v, want the hand-back to empty it", got)
	}
	if got := inv.Slots[player.HotbarSlot]; got.Count != 3 {
		t.Errorf("hotbar slot = %+v, want the three planks handed back", got)
	}
	// The client keeps drawing the stale result unless the failure path
	// refreshes it.
	if !slices.Contains(outcome.Announce, 0) {
		t.Errorf("the result slot was not refreshed, announced %v", outcome.Announce)
	}
}

// TestPlaceRecipeMovesTheStackWhole checks the placement moves the stack it
// takes with its components: the planks' repair cost rides into every grid
// cell rather than being dropped at the cell's edge.
func TestPlaceRecipeMovesTheStackWhole(t *testing.T) {
	inv := &player.Inventory{}
	planks := regItemID(t, "minecraft:oak_planks")
	inv.Set(player.HotbarSlot, protocol.ItemStack{ItemID: planks, Count: 8, RepairCost: 3})
	view := View{Kind: KindPlayer}
	outcome := Outcome{}

	if !PlaceRecipe(view, inv, craftingTableDisplay(t), true, false, &outcome) {
		t.Fatal("a placement the inventory could pay was reported unplaceable")
	}
	for cell := range playerGridSlots {
		got := inv.Slots[playerCraftingGrid+cell]
		if got.Count != 2 || got.RepairCost != 3 {
			t.Errorf("grid cell %d = %+v, want two planks with the repair cost kept", cell, got)
		}
	}
}

// regItemID looks an item's registry id up for the tests.
func regItemID(t *testing.T, name string) int32 {
	t.Helper()
	id, ok := registry.ItemID(name)
	if !ok {
		t.Fatalf("no %s in the item registry", name)
	}
	return id
}
