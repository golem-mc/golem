package rules

import (
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/registry"
	"math/rand"
	"testing"
)

// TestExplorationMapLootFillsTheStackItIsGiven: the map data lands on the
// rolled item, whichever it is, and a failed search leaves the stack bare for
// the table's map_id filter to discard.
// vanilla: ExplorationMapFunction.run, chests/shipwreck_map.json
func TestExplorationMapLootFillsTheStackItIsGiven(t *testing.T) {
	calls := 0
	ctx := &LootContext{ExplorationMap: func(_ registry.LootExplorationMap, stack *protocol.ItemStack) bool {
		calls++
		stack.MapDecorations = map[string]protocol.MapDecoration{"+": {Type: "minecraft:red_x", X: 100, Z: 200, Rotation: 180}}
		stack.SetMapID(4)
		return true
	}}
	functions := []registry.LootFunction{registry.LootExplorationMap{},
		registry.LootFiltered{Predicate: registry.LootItemPredicate{HasMapID: true}, OnFail: registry.LootDiscard{}}}
	table := entryTable("minecraft:buried_treasure_map", functions)
	result := RollLootTable("test", table, ctx, rand.New(rand.NewSource(1)))
	if len(result) != 1 || result[0].ItemID != int32(registry.ItemBuriedTreasureMap) || len(result[0].MapDecorations) != 1 {
		t.Fatalf("loot %+v", result)
	}
	if id, ok := result[0].MapIDOf(); !ok || id != 4 {
		t.Fatal("loot lost map ID")
	}
	traded, _ := ApplyTradeModifiers(functions, protocol.ItemStack{ItemID: int32(registry.ItemTaigaVillageMap), Count: 1}, rand.New(rand.NewSource(1)), ctx)
	if id, ok := traded.MapIDOf(); !ok || id != 4 || traded.ItemID != int32(registry.ItemTaigaVillageMap) {
		t.Fatalf("trade result %+v", traded)
	}
	if calls != 2 {
		t.Fatalf("search ran %d times, want 2", calls)
	}
	ctx.ExplorationMap = func(registry.LootExplorationMap, *protocol.ItemStack) bool { return false }
	if failed := RollLootTable("test", table, ctx, rand.New(rand.NewSource(1))); len(failed) != 0 {
		t.Fatalf("failed lookup loot %+v", failed)
	}
}
