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

// regItemID looks an item's registry id up for the tests.
func regItemID(t *testing.T, name string) int32 {
	t.Helper()
	id, ok := registry.ItemID(name)
	if !ok {
		t.Fatalf("no %s in the item registry", name)
	}
	return id
}
