package inventory

import (
	"math/rand"
	"slices"
	"testing"

	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

func trade(wants, gives string, variants ...string) registry.VillagerTrade {
	return registry.VillagerTrade{
		Wants: registry.TradeCost{Item: "minecraft:" + wants, Count: 3}, Gives: registry.TradeCost{Item: "minecraft:" + gives, Count: 1},
		MaxUses: 12, XP: 2, Discount: 0.05, Variants: variants,
	}
}

func sells(t *testing.T, tag nbt.Compound) []string {
	t.Helper()
	recipes, _ := nbt.Get[nbt.List](tag, "Recipes")
	var out []string
	for _, r := range recipes.Items {
		sell, _ := nbt.Get[nbt.Compound](r.(nbt.Compound), "sell")
		out = append(out, string(sell["id"].(nbt.String)))
	}
	return out
}

func tradeRandom() *rand.Rand { return rand.New(rand.NewSource(1)) }

func TestDrawOffersWithoutDuplicatesTakesEachTradeOnce(t *testing.T) {
	set := registry.TradeSet{Amount: 3, Trades: []registry.VillagerTrade{trade("coal", "bread"), trade("coal", "apple")}}
	got := sells(t, DrawOffers(nil, set, "minecraft:plains", tradeRandom(), nil))
	if len(got) != 2 || !slices.Contains(got, "minecraft:bread") || !slices.Contains(got, "minecraft:apple") {
		t.Errorf("offers %v, want bread and apple", got)
	}
}

func TestDrawOffersAddsAndDoublesTheEnchantedBookPrice(t *testing.T) {
	tr := trade("emerald", "book")
	tr.Wants.Count = 1
	tr.Modifiers = []registry.LootFunction{registry.LootEnchantRandomly{
		Options: []string{"minecraft:mending"}, IncludeAdditionalCost: true,
	}}
	var plain int32
	for _, double := range []bool{false, true} {
		if double {
			tr.DoublePrice = []string{"minecraft:mending"}
		}
		tag := DrawOffers(nil, registry.TradeSet{Amount: 1, Trades: []registry.VillagerTrade{tr}}, "minecraft:plains", tradeRandom(), nil)
		offers, _ := OffersFromTag(tag)
		if len(offers) != 1 {
			t.Fatalf("offers %v", offers)
		}
		o := offers[0]
		if double && o.CostA.Count != 1+2*(plain-1) {
			t.Errorf("doubled cost %d, base %d", o.CostA.Count, plain)
		}
		if !double {
			plain = o.CostA.Count
		}
		mending, _ := registry.EnchantmentID("minecraft:mending")
		if plain < 6 || plain > 20 || o.Result.StoredEnchantments[mending] != 1 {
			t.Errorf("offer %+v", o)
		}
		recipes, _ := nbt.Get[nbt.List](tag, "Recipes")
		sell, _ := nbt.Get[nbt.Compound](recipes.Items[0].(nbt.Compound), "sell")
		components, _ := nbt.Get[nbt.Compound](sell, "components")
		if _, ok := components["minecraft:additional_trade_cost"]; ok {
			t.Fatal("temporary surcharge leaked into the sold book")
		}
	}
	// The surcharge, including any doubling, is capped by the wanted stack size.
	tr.Wants.Count = 60
	tag := DrawOffers(nil, registry.TradeSet{Amount: 1, Trades: []registry.VillagerTrade{tr}}, "minecraft:plains", tradeRandom(), nil)
	offers, _ := OffersFromTag(tag)
	if offers[0].CostA.Count != 64 {
		t.Errorf("cost %d, want 64", offers[0].CostA.Count)
	}
}

func TestDrawOffersFromComponentTradesKeepsTheirComponents(t *testing.T) {
	for _, name := range []string{"leatherworker/level_1", "leatherworker/level_2", "leatherworker/level_3", "leatherworker/level_4", "leatherworker/level_5", "farmer/level_4", "fletcher/level_5", "wandering_trader/uncommon"} {
		t.Run(name, func(t *testing.T) {
			set, ok := registry.FindTradeSet(name)
			if !ok {
				t.Fatal("no trade set")
			}
			set.Amount, set.Duplicates = len(set.Trades), false
			tag := DrawOffers(nil, set, "minecraft:plains", tradeRandom(), nil)
			offers, _ := OffersFromTag(tag)
			if len(offers) != len(set.Trades) {
				t.Fatalf("offers %d, trades %d", len(offers), len(set.Trades))
			}
			components := 0
			for _, o := range offers {
				item, _ := registry.ItemName(o.Result.ItemID)
				switch item {
				case "minecraft:leather_leggings", "minecraft:leather_chestplate", "minecraft:leather_boots", "minecraft:leather_helmet", "minecraft:leather_horse_armor":
					if !o.Result.HasDyedColor() {
						t.Fatal("undyed leather trade")
					}
					components++
				case "minecraft:suspicious_stew":
					effects := o.Result.SuspiciousStewEffectsOf()
					durations := map[int32]int32{registry.MustMobEffect("minecraft:night_vision"): 100, registry.MustMobEffect("minecraft:jump_boost"): 160, registry.MustMobEffect("minecraft:weakness"): 140, registry.MustMobEffect("minecraft:blindness"): 120, registry.MustMobEffect("minecraft:poison"): 280, registry.MustMobEffect("minecraft:saturation"): 7}
					if len(effects) != 1 || durations[effects[0].Effect] != effects[0].Duration {
						t.Fatalf("stew effects %v", effects)
					}
					components++
				case "minecraft:tipped_arrow":
					contents, ok := o.Result.PotionContentsOf()
					names, _ := registry.PotionTagMembers("minecraft:tradeable")
					potion, _ := registry.PotionName(contents.Potion)
					if !ok || !slices.Contains(names, potion) || o.Result.Count != 5 || o.CostB.Count != 5 || o.CostA.Count != 2 {
						t.Fatalf("arrow offer %+v", o)
					}
					components++
				case "minecraft:potion":
					contents, ok := o.Result.PotionContentsOf()
					if !ok || contents.Potion != registry.MustPotion("minecraft:long_invisibility") {
						t.Fatalf("potion %+v", contents)
					}
					components++
				}
			}
			if components == 0 {
				t.Fatal("no component-bearing offer")
			}
		})
	}
}

func TestDrawOffersDoublesThePriceForTheLoadedDoublePriceTag(t *testing.T) {
	tr := trade("emerald", "book")
	tr.Wants.Count = 1
	tr.DoublePriceTag = "minecraft:double_trade_price"
	tr.Modifiers = []registry.LootFunction{registry.LootEnchantRandomly{
		Options: []string{"minecraft:mending"}, IncludeAdditionalCost: true,
	}}
	cost := func() int32 {
		tag := DrawOffers(nil, registry.TradeSet{Amount: 1, Trades: []registry.VillagerTrade{tr}}, "minecraft:plains", tradeRandom(), nil)
		offers, _ := OffersFromTag(tag)
		if len(offers) != 1 {
			t.Fatalf("offers %v", offers)
		}
		return offers[0].CostA.Count
	}
	replace := func(member string) {
		registry.ApplyPackTags(registry.PackTags{"minecraft:enchantment": {
			"minecraft:double_trade_price": {Replace: true, Entries: []registry.PackTagEntry{{ID: member, Required: true}}},
		}})
	}
	t.Cleanup(func() { registry.ApplyPackTags(nil) })

	replace("minecraft:unbreaking")
	plain := cost()
	replace("minecraft:mending")
	if doubled := cost(); doubled != 1+2*(plain-1) {
		t.Errorf("cost with mending in the tag %d, want %d", doubled, 1+2*(plain-1))
	}
}
