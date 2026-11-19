package registry

import "testing"

func TestBuildTradeSetsLayersPackTrades(t *testing.T) {
	stick := VillagerTrade{Wants: TradeCost{Item: "minecraft:emerald", Count: 1}, Gives: TradeCost{Item: "minecraft:stick", Count: 1}}
	trades := map[string]VillagerTrade{"pk:stick": stick}
	builtin := tradeSets["armorer/level_1"]

	added, problems := BuildTradeSets(trades, map[string]PackTag{
		"minecraft:armorer/level_1": {Entries: []PackTagEntry{{ID: "pk:stick", Required: true}}},
	}, nil)
	got := added["armorer/level_1"].Trades
	if len(problems) != 0 || len(got) != len(builtin.Trades)+1 || got[len(got)-1].Gives.Item != "minecraft:stick" {
		t.Fatalf("added tag: %d trades (built-in %d), problems %v", len(got), len(builtin.Trades), problems)
	}

	_, problems = BuildTradeSets(nil, map[string]PackTag{
		"minecraft:armorer/level_1": {Entries: []PackTagEntry{{ID: "pk:missing", Required: true}}},
	}, nil)
	if len(problems) == 0 {
		t.Fatal("a tag naming a missing trade loaded")
	}
}
