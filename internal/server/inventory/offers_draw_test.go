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

func TestDrawOffersWithDuplicatesFillsTheAmount(t *testing.T) {
	set := registry.TradeSet{Amount: 3, Duplicates: true, Trades: []registry.VillagerTrade{trade("coal", "bread")}}
	if got := sells(t, DrawOffers(nil, set, "minecraft:plains", tradeRandom(), nil)); len(got) != 3 {
		t.Errorf("%d offers, want 3", len(got))
	}
}

func TestDrawOffersSkipsTradesOfOtherVariantsAndUnsupportedOnes(t *testing.T) {
	unsupported := trade("coal", "apple")
	unsupported.Unsupported = true
	set := registry.TradeSet{Amount: 3, Trades: []registry.VillagerTrade{
		trade("coal", "bread", "minecraft:swamp"), unsupported, trade("coal", "cake", "minecraft:plains", "minecraft:snow"), trade("coal", "pumpkin_pie"),
	}}
	got := sells(t, DrawOffers(nil, set, "minecraft:plains", tradeRandom(), nil))
	if len(got) != 2 || !slices.Contains(got, "minecraft:cake") || !slices.Contains(got, "minecraft:pumpkin_pie") {
		t.Errorf("offers %v, want cake and pie", got)
	}
}

func TestDrawOffersWritesTheSavedRecipe(t *testing.T) {
	tr := trade("wheat", "emerald")
	tr.Wants.Count = 200
	tr.Extra = registry.TradeCost{Item: "minecraft:coal", Count: 2}
	tr.MaxUses = 0
	tag := DrawOffers(nil, registry.TradeSet{Amount: 1, Trades: []registry.VillagerTrade{tr}}, "minecraft:plains", tradeRandom(), nil)
	offers, _ := OffersFromTag(tag)
	if len(offers) != 1 {
		t.Fatalf("%d offers read back, want 1", len(offers))
	}
	o := offers[0]
	if o.CostA.Count != 64 || o.CostB.Count != 2 || o.MaxUses != 1 || o.XP != 2 || o.PriceMultiplier != 0.05 {
		t.Errorf("offer %+v, want cost clamped to a stack, second cost, max uses at least 1", o)
	}
}

// vanilla: MerchantOffer.CODEC
func TestDrawOffersOmitsCodecDefaults(t *testing.T) {
	tr := trade("wheat", "emerald")
	tr.MaxUses, tr.XP, tr.Discount = 4, 1, 0
	tag := DrawOffers(nil, registry.TradeSet{Amount: 1, Trades: []registry.VillagerTrade{tr}}, "minecraft:plains", tradeRandom(), nil)
	recipes, _ := nbt.Get[nbt.List](tag, "Recipes")
	if len(recipes.Items) != 1 {
		t.Fatalf("drawn recipes = %v, want one", recipes.Items)
	}
	recipe := recipes.Items[0].(nbt.Compound)
	if len(recipe) != 2 || recipe["buy"] == nil || recipe["sell"] == nil {
		t.Errorf("drawn recipe = %v, want only buy and sell", recipe)
	}
	offers, _ := OffersFromTag(tag)
	if len(offers) != 1 || offers[0].MaxUses != 4 || offers[0].XP != 1 || !offers[0].RewardExp {
		t.Errorf("read drawn recipe = %+v, want codec defaults", offers)
	}
}

func TestDrawOffersTakesTheDrawnIndex(t *testing.T) {
	set := registry.TradeSet{Amount: 1, Trades: []registry.VillagerTrade{trade("coal", "bread"), trade("coal", "apple"), trade("coal", "cake")}}
	got := sells(t, DrawOffers(nil, set, "minecraft:plains", tradeRandom(), nil))
	if len(got) != 1 || got[0] != set.Trades[tradeRandom().Intn(len(set.Trades))].Gives.Item {
		t.Errorf("offers %v, want the randomly selected trade", got)
	}
}

func TestDrawOffersWithDuplicatesDropsATradeThatMakesNoOffer(t *testing.T) {
	unsupported := trade("coal", "apple")
	unsupported.Unsupported = true
	set := registry.TradeSet{Amount: 2, Duplicates: true, Trades: []registry.VillagerTrade{unsupported, trade("coal", "bread")}}
	got := sells(t, DrawOffers(nil, set, "minecraft:plains", tradeRandom(), nil))
	if len(got) != 2 || got[0] != "minecraft:bread" || got[1] != "minecraft:bread" {
		t.Errorf("offers %v, want two bread once the unsupported trade left the pool", got)
	}
}

func TestDrawOffersFromTheRealBoatTradesKeepsOnlyTheVillagersVariant(t *testing.T) {
	set, ok := registry.FindTradeSet("fisherman/level_5")
	if !ok {
		t.Fatal("no fisherman/level_5 trade set")
	}
	set.Amount = len(set.Trades)
	var wants []string
	recipes, _ := nbt.Get[nbt.List](DrawOffers(nil, set, "minecraft:swamp", tradeRandom(), nil), "Recipes")
	for _, r := range recipes.Items {
		buy, _ := nbt.Get[nbt.Compound](r.(nbt.Compound), "buy")
		wants = append(wants, string(buy["id"].(nbt.String)))
	}
	if !slices.Contains(wants, "minecraft:dark_oak_boat") || slices.Contains(wants, "minecraft:oak_boat") || slices.Contains(wants, "minecraft:spruce_boat") {
		t.Errorf("wants %v, want the swamp boat and no other variant's", wants)
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

func TestDrawOffersEnablesTheVanillaEnchantedTrades(t *testing.T) {
	for _, name := range []string{"armorer/level_4", "armorer/level_5", "fisherman/level_3", "fletcher/level_4",
		"fletcher/level_5", "librarian/level_1", "librarian/level_2", "librarian/level_3", "librarian/level_4",
		"toolsmith/level_3", "toolsmith/level_4", "toolsmith/level_5", "weaponsmith/level_1", "weaponsmith/level_5"} {
		t.Run(name, func(t *testing.T) {
			set, ok := registry.FindTradeSet(name)
			if !ok {
				t.Fatal("missing trade set")
			}
			set.Amount = len(set.Trades)
			tag := DrawOffers(nil, set, "minecraft:plains", tradeRandom(), nil)
			offers, _ := OffersFromTag(tag)
			enchanted := 0
			for _, o := range offers {
				if len(o.Result.Enchantments)+len(o.Result.StoredEnchantments) == 0 {
					continue
				}
				enchanted++
				if o.CostA.Count < 6 || o.CostA.Count > 64 {
					t.Errorf("enchanted price %d", o.CostA.Count)
				}
			}
			if enchanted == 0 {
				t.Fatal("no enchanted offer")
			}
		})
	}
}

func TestDrawOffersDropsFailedModifiersAndKeepsTheSavedInput(t *testing.T) {
	tr := trade("emerald", "book")
	tr.Modifiers = []registry.LootFunction{registry.LootDiscard{}}
	old := nbt.Compound{"Recipes": nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{nbt.Compound{"marker": nbt.Int(1)}}}}
	set := registry.TradeSet{Amount: 2, Duplicates: true, Trades: []registry.VillagerTrade{tr, trade("coal", "bread")}}
	got := DrawOffers(old, set, "minecraft:plains", tradeRandom(), nil)
	before, _ := nbt.Get[nbt.List](old, "Recipes")
	after, _ := nbt.Get[nbt.List](got, "Recipes")
	if len(before.Items) != 1 || len(after.Items) != 3 {
		t.Fatalf("recipe counts %d -> %d", len(before.Items), len(after.Items))
	}
	offers, _ := OffersFromTag(got)
	if len(offers) != 2 {
		t.Fatalf("%d offers, want two bread", len(offers))
	}
	for _, o := range offers {
		name, _ := registry.ItemName(o.Result.ItemID)
		if name != "minecraft:bread" {
			t.Errorf("result %s", name)
		}
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
