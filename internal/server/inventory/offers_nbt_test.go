package inventory

import (
	"reflect"
	"testing"

	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// vanilla: MerchantOffer.CODEC, NbtOps.getNumberValue, NbtOps.getBooleanValue
func TestOffersFromTagReadsNumericCodecValues(t *testing.T) {
	for name, number := range map[string]nbt.Tag{
		"byte": nbt.Byte(2), "short": nbt.Short(2), "int": nbt.Int(2),
		"long": nbt.Long(2), "float": nbt.Float(2.75), "double": nbt.Double(2.75),
		"invalid": nbt.String("2"),
	} {
		t.Run(name, func(t *testing.T) {
			recipe := recipeOf("minecraft:emerald", "minecraft:bread", 0)
			for _, key := range []string{"uses", "maxUses", "xp", "specialPrice", "demand", "priceMultiplier", "rewardExp"} {
				recipe[key] = number
			}
			offer, ok := offerFromTag(recipe)
			if !ok {
				t.Fatal("numeric recipe was rejected")
			}
			want := Offer{CostA: offer.CostA, Result: offer.Result, Uses: 2, MaxUses: 2, XP: 2, SpecialPrice: 2, Demand: 2, PriceMultiplier: 2, RewardExp: true}
			if name == "float" || name == "double" {
				want.PriceMultiplier = 2.75
			} else if name == "invalid" {
				want = Offer{CostA: offer.CostA, Result: offer.Result, MaxUses: 4, XP: 1, RewardExp: true}
			}
			if !reflect.DeepEqual(offer, want) {
				t.Errorf("offer = %+v, want %+v", offer, want)
			}
		})
	}
	for _, reward := range []nbt.Tag{nbt.Short(0), nbt.Int(0), nbt.Long(0), nbt.Float(0), nbt.Double(0), nbt.Double(0.5)} {
		recipe := recipeOf("minecraft:emerald", "minecraft:bread", 0)
		recipe["rewardExp"] = reward
		offer, ok := offerFromTag(recipe)
		if !ok || offer.RewardExp != (reward == nbt.Double(0.5)) {
			t.Errorf("rewardExp %v: offer = %+v, ok = %v", reward, offer, ok)
		}
	}
}

// vanilla: MerchantOffer.CODEC
func TestWriteOfferStateOmitsAndRemovesCodecDefaults(t *testing.T) {
	base := nbt.Compound{
		"buy":  nbt.Compound{"id": nbt.String("minecraft:emerald"), "count": nbt.Int(1)},
		"buyB": nbt.Compound{"id": nbt.String("minecraft:book"), "count": nbt.Int(1)},
		"sell": nbt.Compound{"id": nbt.String("minecraft:bread"), "count": nbt.Int(6)},
	}
	tag := nbt.Compound{"Recipes": nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{base}}}
	offers, saved := OffersFromTag(tag)
	if len(offers) != 1 {
		t.Fatalf("read %d offers, want one", len(offers))
	}
	defaults := offers[0]
	if defaults.Uses != 0 || defaults.MaxUses != 4 || !defaults.RewardExp || defaults.SpecialPrice != 0 || defaults.Demand != 0 || defaults.PriceMultiplier != 0 || defaults.XP != 1 {
		t.Fatalf("codec defaults = %+v", defaults)
	}
	check := func(tag nbt.Compound, want nbt.Compound) {
		t.Helper()
		recipes, _ := nbt.Get[nbt.List](tag, "Recipes")
		if !reflect.DeepEqual(recipes.Items[0], want) {
			t.Errorf("saved recipe = %v, want %v", recipes.Items[0], want)
		}
	}
	check(WriteOfferState(tag, offers, saved), base)
	offers[0].Uses, offers[0].MaxUses, offers[0].RewardExp = 1, 12, false
	offers[0].SpecialPrice, offers[0].Demand, offers[0].PriceMultiplier, offers[0].XP = -2, 3, 0.05, 2
	nondefaults := WriteOfferState(tag, offers, saved)
	want := nbt.Copy(base).(nbt.Compound)
	want["uses"], want["maxUses"], want["rewardExp"] = nbt.Int(1), nbt.Int(12), nbt.Byte(0)
	want["specialPrice"], want["demand"], want["priceMultiplier"], want["xp"] = nbt.Int(-2), nbt.Int(3), nbt.Float(0.05), nbt.Int(2)
	check(nondefaults, want)
	reread, _ := OffersFromTag(nondefaults)
	if !reflect.DeepEqual(reread, offers) {
		t.Errorf("nondefault round trip = %+v, want %+v", reread, offers)
	}
	check(WriteOfferState(nondefaults, []Offer{defaults}, saved), base)
	check(nondefaults, want)
	check(tag, base)
}

func recipeOf(buy, sell string, uses int32) nbt.Compound {
	return nbt.Compound{
		"buy":     nbt.Compound{"id": nbt.String(buy), "count": nbt.Int(2)},
		"sell":    nbt.Compound{"id": nbt.String(sell), "count": nbt.Int(6)},
		"uses":    nbt.Int(uses),
		"maxUses": nbt.Int(16),
		"xp":      nbt.Int(5),
	}
}

func TestOffersFromTagSkipsUnreadableRecipesAndKeepsTheirIndexes(t *testing.T) {
	tag := nbt.Compound{"Recipes": nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{
		nbt.Compound{"buy": nbt.Compound{"id": nbt.String("minecraft:emerald")}},
		recipeOf("minecraft:emerald", "minecraft:bread", 3),
	}}}
	offers, saved := OffersFromTag(tag)
	if len(offers) != 1 || len(saved) != 1 || saved[0] != 1 {
		t.Fatalf("offers %d, saved %v; want the one readable recipe at index 1", len(offers), saved)
	}
	if offers[0].Uses != 3 || offers[0].MaxUses != 16 || offers[0].XP != 5 || offers[0].Result.ItemID != registry.MustItem("minecraft:bread") {
		t.Errorf("offer read as %+v", offers[0])
	}
}

func TestWriteOfferStateWritesBackOnlyTheTradedNumbers(t *testing.T) {
	tag := nbt.Compound{"Recipes": nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{
		nbt.Compound{"buy": nbt.Compound{"id": nbt.String("minecraft:emerald")}},
		recipeOf("minecraft:emerald", "minecraft:bread", 3),
	}}}
	offers, saved := OffersFromTag(tag)
	offers[0].Uses, offers[0].Demand, offers[0].SpecialPrice = 4, 2, -1
	out := WriteOfferState(tag, offers, saved)

	recipes, _ := nbt.Get[nbt.List](out, "Recipes")
	if len(recipes.Items) != 2 {
		t.Fatalf("%d recipes written, want the unreadable one kept too", len(recipes.Items))
	}
	got, _ := recipes.Items[1].(nbt.Compound)
	for key, want := range map[string]nbt.Int{"uses": 4, "demand": 2, "specialPrice": -1, "maxUses": 16, "xp": 5} {
		if v, _ := nbt.Get[nbt.Int](got, key); v != want {
			t.Errorf("%s = %d, want %d", key, v, want)
		}
	}
	orig, _ := tag["Recipes"].(nbt.List)
	if c, _ := orig.Items[1].(nbt.Compound); c["uses"] != nbt.Int(3) {
		t.Error("the input tag was modified")
	}
}

func TestWriteOfferStateIgnoresSavedIndexesPastTheRecipeList(t *testing.T) {
	tag := nbt.Compound{"Recipes": nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{
		recipeOf("minecraft:emerald", "minecraft:bread", 3),
	}}}
	offers, _ := OffersFromTag(tag)
	offers[0].Uses = 9
	for name, saved := range map[string][]int{"past the end": {4}, "negative": {-1}} {
		out := WriteOfferState(tag, offers, saved)
		recipes, _ := nbt.Get[nbt.List](out, "Recipes")
		got, _ := recipes.Items[0].(nbt.Compound)
		if len(recipes.Items) != 1 || got["uses"] != nbt.Int(3) {
			t.Errorf("%s: recipes %v, want the one recipe untouched", name, recipes.Items)
		}
	}
	empty := nbt.Compound{"Recipes": nbt.List{Elem: nbt.TypeCompound}}
	out := WriteOfferState(empty, offers, []int{0})
	if recipes, _ := nbt.Get[nbt.List](out, "Recipes"); len(recipes.Items) != 0 {
		t.Errorf("recipes %v written for an empty list", recipes.Items)
	}
}

func TestUpdateOfferStatePreservesUnshowableRecipes(t *testing.T) {
	recipe := recipeOf("minecraft:emerald", "minecraft:bread", 3)
	recipe["buy"].(nbt.Compound)["count"] = nbt.Byte(2)
	recipe["buy"].(nbt.Compound)["components"] = nbt.Compound{
		"minecraft:custom_data": nbt.Compound{"marker": nbt.Int(1)},
	}
	recipe["buyB"] = nbt.Compound{"id": nbt.String("minecraft:book"), "count": nbt.Int(1)}
	recipe["uses"], recipe["maxUses"], recipe["demand"] = nbt.Double(3.75), nbt.Byte(6), nbt.Long(3)
	recipe["priceMultiplier"], recipe["rewardExp"] = nbt.Double(0.25), nbt.Short(0)
	recipe["specialPrice"], recipe["xp"] = nbt.Short(-2), nbt.Byte(5)
	tag := nbt.Compound{"Recipes": nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{recipe}}}
	if offers, _ := OffersFromTag(tag); len(offers) != 0 {
		t.Fatal("the menu reader unexpectedly supports this cost predicate")
	}
	original := nbt.Copy(tag)
	out := UpdateOfferState(tag, func(uses, maxUses, demand int32) (int32, int32) {
		return 0, demand + uses - (maxUses - uses)
	})
	want := nbt.Copy(recipe).(nbt.Compound)
	want["buy"].(nbt.Compound)["count"] = nbt.Int(2)
	delete(want, "uses")
	want["maxUses"], want["demand"], want["xp"] = nbt.Int(6), nbt.Int(3), nbt.Int(5)
	want["specialPrice"], want["priceMultiplier"], want["rewardExp"] = nbt.Int(-2), nbt.Float(0.25), nbt.Byte(0)
	if got := out["Recipes"].(nbt.List).Items[0]; !reflect.DeepEqual(nbt.PlainCopy(got), want) {
		t.Errorf("updated recipe = %v, want %v", got, want)
	}
	if !reflect.DeepEqual(tag, original) {
		t.Fatal("updating offer state modified the original recipes")
	}
}

func TestOrderedOfferRecipesRestockAndWriteBack(t *testing.T) {
	recipe := recipeOf("minecraft:emerald", "minecraft:bread", 3)
	recipe["maxUses"], recipe["demand"] = nbt.Byte(6), nbt.Long(3)
	tag := nbt.Compound{"Recipes": nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{nbt.OrderedCopy(recipe)}}}
	original := nbt.Copy(tag)
	offers, saved := OffersFromTag(tag)
	if len(offers) != 1 {
		t.Fatalf("read %d ordered offers, want one", len(offers))
	}
	offers[0].Uses = 4
	for name, out := range map[string]nbt.Compound{
		"trade": WriteOfferState(tag, offers, saved),
		"restock": UpdateOfferState(tag, func(uses, maxUses, demand int32) (int32, int32) {
			return 0, demand + uses - (maxUses - uses)
		}),
	} {
		t.Run(name, func(t *testing.T) {
			got, _ := OffersFromTag(out)
			want := offers[0]
			if name == "restock" {
				want.Uses = 0
			}
			if len(got) != 1 || !reflect.DeepEqual(got[0], want) {
				t.Errorf("updated offers = %+v, want %+v", got, want)
			}
			recipes := out["Recipes"].(nbt.List)
			updated, _ := nbt.CompoundView(recipes.Items[0])
			if name == "restock" {
				if _, exists := updated["uses"]; exists {
					t.Error("restock retained default uses")
				}
			}
			if _, ok := updated["buy"].(*nbt.OrderedCompound); !ok {
				t.Error("updating offer state stripped nested compound ordering")
			}
		})
	}
	if !reflect.DeepEqual(tag, original) {
		t.Error("updating offer state modified the original ordered recipes")
	}
}

// vanilla: ItemCost.CODEC, ExtraCodecs.optionalAlwaysPresentFieldOf
func TestSavedOfferCostCountsUseIntegerTags(t *testing.T) {
	for name, number := range map[string]nbt.Tag{
		"byte": nbt.Byte(2), "short": nbt.Short(2), "int": nbt.Int(2),
		"long": nbt.Long(2), "float": nbt.Float(2.75), "double": nbt.Double(2.75),
		"missing": nil,
	} {
		t.Run(name, func(t *testing.T) {
			recipe := recipeOf("minecraft:emerald", "minecraft:bread", 3)
			cost := recipe["buy"].(nbt.Compound)
			cost["count"] = number
			if number == nil {
				delete(cost, "count")
			}
			recipe["buyB"] = nbt.Copy(cost)
			tag := nbt.Compound{"Recipes": nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{nbt.OrderedCopy(recipe)}}}
			original := nbt.Copy(tag)
			offers, saved := OffersFromTag(tag)
			if len(offers) != 1 {
				t.Fatal("cost was not read")
			}
			for method, out := range map[string]nbt.Compound{
				"trade":   WriteOfferState(tag, offers, saved),
				"restock": UpdateOfferState(tag, func(uses, _, demand int32) (int32, int32) { return uses, demand }),
			} {
				got, _ := nbt.CompoundView(out["Recipes"].(nbt.List).Items[0])
				want := nbt.Int(2)
				if number == nil {
					want = 1
				}
				for _, key := range []string{"buy", "buyB"} {
					cost, _ := nbt.Get[nbt.Compound](got, key)
					if cost["count"] != want {
						t.Errorf("%s %s count = %v (%T), want integer %d", method, key, cost["count"], cost["count"], want)
					}
					if _, ok := got[key].(*nbt.OrderedCompound); !ok {
						t.Errorf("%s %s lost compound ordering", method, key)
					}
				}
				reread, _ := OffersFromTag(out)
				if !reflect.DeepEqual(reread, offers) {
					t.Errorf("%s changed the trade: %+v, want %+v", method, reread, offers)
				}
			}
			if !reflect.DeepEqual(tag, original) {
				t.Fatal("normalizing cost counts modified the input")
			}
		})
	}
}
