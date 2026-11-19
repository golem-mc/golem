package inventory

import (
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/registry"
	"testing"
)

// TestExplorerMapOffersUseTheWorldAndDropFailedSearches: each destination's
// map is an item of its own, sold with the map the search made on it.
// vanilla: villager_trade/cartographer/2, ExplorationMapFunction.run
func TestExplorerMapOffersUseTheWorldAndDropFailedSearches(t *testing.T) {
	set, _ := registry.FindTradeSet("cartographer/level_2")
	maps := 0
	for _, trade := range set.Trades {
		if len(trade.Modifiers) == 0 {
			continue
		}
		if _, ok := trade.Modifiers[0].(registry.LootExplorationMap); !ok {
			t.Fatalf("%s is modified by %T first, want the map search", trade.Gives.Item, trade.Modifiers[0])
		}
		sold, ok := registry.ItemID(trade.Gives.Item)
		if !ok || registry.Item(sold) == registry.ItemMap || registry.Item(sold) == registry.ItemFilledMap {
			t.Fatalf("the trade sells %s, want a destination's own map item", trade.Gives.Item)
		}
		maps++
		single := registry.TradeSet{Amount: 1, Trades: []registry.VillagerTrade{trade}}
		var calls int
		ctx := &rules.LootContext{ExplorationMap: func(fn registry.LootExplorationMap, stack *protocol.ItemStack) bool {
			calls++
			if len(fn.Destination) == 0 || fn.SearchRadius != 100 || !fn.SkipExistingChunks {
				t.Fatalf("map search parameters %+v", fn)
			}
			stack.SetMapID(7)
			return true
		}}
		tag := DrawOffers(nil, single, trade.Variants[0], tradeRandom(), ctx)
		offers, _ := OffersFromTag(tag)
		if calls != 1 || len(offers) != 1 {
			t.Fatalf("searches %d, offers %d", calls, len(offers))
		}
		if id, ok := offers[0].Result.MapIDOf(); !ok || id != 7 {
			t.Fatal("offer lost map ID")
		}
		if offers[0].Result.ItemID != sold {
			t.Fatalf("the offer sells item %d, want %s", offers[0].Result.ItemID, trade.Gives.Item)
		}
		failed := DrawOffers(nil, single, trade.Variants[0], tradeRandom(), nil)
		if offers, _ := OffersFromTag(failed); len(offers) != 0 {
			t.Fatal("failed search produced an offer")
		}
	}
	if maps != 7 {
		t.Fatalf("%d village and biome maps, want 7", maps)
	}
}
