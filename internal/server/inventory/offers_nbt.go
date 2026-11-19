package inventory

import (
	"maps"

	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// OffersFromTag reads a villager's saved Offers, leaving out recipes this
// server cannot show; saved maps each kept offer to its saved index for
// WriteOfferState.
// vanilla: MerchantOffer.CODEC
func OffersFromTag(tag nbt.Compound) (offers []Offer, saved []int) {
	recipes, _ := nbt.Get[nbt.List](tag, "Recipes")
	for i, item := range recipes.Items {
		recipe, ok := nbt.CompoundView(item)
		if !ok {
			continue
		}
		offer, ok := offerFromTag(recipe)
		if !ok {
			continue
		}
		offers = append(offers, offer)
		saved = append(saved, i)
	}
	return offers, saved
}

func offerFromTag(recipe nbt.Compound) (Offer, bool) {
	buy, ok := nbt.Get[nbt.Compound](recipe, "buy")
	if !ok {
		return Offer{}, false
	}
	costA, ok := costFromTag(buy)
	if !ok {
		return Offer{}, false
	}
	sell, ok := nbt.Get[nbt.Compound](recipe, "sell")
	if !ok {
		return Offer{}, false
	}
	result, ok := registry.StoredStack(sell)
	if !ok {
		return Offer{}, false
	}
	offer := offerFieldsFromTag(recipe)
	offer.CostA, offer.Result = costA, result
	if buyB, ok := nbt.Get[nbt.Compound](recipe, "buyB"); ok {
		if offer.CostB, ok = costFromTag(buyB); !ok {
			return Offer{}, false
		}
	}
	return offer, true
}

func offerFieldsFromTag(recipe nbt.Compound) Offer {
	offer := Offer{
		MaxUses:         intOf(recipe, "maxUses", 4),
		Uses:            intOf(recipe, "uses", 0),
		XP:              intOf(recipe, "xp", 1),
		SpecialPrice:    intOf(recipe, "specialPrice", 0),
		Demand:          intOf(recipe, "demand", 0),
		PriceMultiplier: 0,
		RewardExp:       true,
	}
	if b, ok := nbt.NumberValue(recipe["rewardExp"]); ok {
		offer.RewardExp = b != 0
	}
	if m, ok := nbt.NumberValue(recipe["priceMultiplier"]); ok {
		offer.PriceMultiplier = float32(m)
	}
	return offer
}

// costFromTag reads an ItemCost: an item id and a count that defaults to 1.
// vanilla: ItemCost.CODEC
func costFromTag(tag nbt.Compound) (Cost, bool) {
	name, _ := nbt.Get[nbt.String](tag, "id")
	id, ok := registry.ItemID(registry.Qualify(string(name)))
	if !ok {
		return Cost{}, false
	}
	cost := Cost{ItemID: id, Count: intOf(tag, "count", 1)}
	if components, ok := nbt.Get[nbt.Compound](tag, "components"); ok && len(components) > 0 {
		// Only the water predicate is modelled; an offer with another is left out.
		if !isWaterContents(components) {
			return Cost{}, false
		}
		cost.Water = true
	}
	return cost, true
}

func isWaterContents(components nbt.Compound) bool {
	contents, ok := nbt.Get[nbt.Compound](components, "minecraft:potion_contents")
	if !ok || len(components) != 1 || len(contents) != 1 {
		return false
	}
	potion, _ := nbt.Get[nbt.String](contents, "potion")
	return potion == "minecraft:water"
}

func intOf(tag nbt.Compound, key string, def int32) int32 {
	if v, ok := nbt.IntValue(tag[key]); ok {
		return v
	}
	return def
}

// UpdateOfferState changes each recipe's uses and demand on a copy of the offers.
// It includes recipes whose item predicates this server cannot show in a menu.
func UpdateOfferState(tag nbt.Compound, update func(uses, maxUses, demand int32) (int32, int32)) nbt.Compound {
	if _, ok := nbt.Get[nbt.List](tag, "Recipes"); !ok {
		return tag
	}
	out, items := copyOfferRecipes(tag)
	for i, item := range items {
		if recipe, ok := nbt.CompoundView(item); ok {
			offer := offerFieldsFromTag(recipe)
			offer.Uses, offer.Demand = update(offer.Uses, offer.MaxUses, offer.Demand)
			items[i] = updatedOfferRecipe(recipe, offer)
		}
	}
	return out
}

// WriteOfferState copies offer state back onto saved recipes and omits codec
// defaults and canonicalizes cost counts. It returns a copy; tag is not modified.
func WriteOfferState(tag nbt.Compound, offers []Offer, saved []int) nbt.Compound {
	out, items := copyOfferRecipes(tag)
	for n, i := range saved {
		if i < 0 || i >= len(items) || n >= len(offers) {
			continue
		}
		if recipe, ok := nbt.CompoundView(items[i]); ok {
			items[i] = updatedOfferRecipe(recipe, offers[n])
		}
	}
	return out
}

// copyOfferRecipes returns a copy whose recipe list shares the returned items.
func copyOfferRecipes(tag nbt.Compound) (nbt.Compound, []nbt.Tag) {
	recipes, _ := nbt.Get[nbt.List](tag, "Recipes")
	items := append([]nbt.Tag(nil), recipes.Items...)
	out := make(nbt.Compound, len(tag))
	maps.Copy(out, tag)
	out["Recipes"] = nbt.List{Elem: recipes.Elem, Items: items}
	return out, items
}

func updatedOfferRecipe(recipe nbt.Compound, offer Offer) nbt.Compound {
	updated := make(nbt.Compound, len(recipe)+3)
	maps.Copy(updated, recipe)
	// vanilla: ItemCost.CODEC always writes count, including its default of 1.
	for _, key := range [...]string{"buy", "buyB"} {
		if cost, ok := nbt.Get[nbt.Compound](recipe, key); ok {
			count := nbt.Int(intOf(cost, "count", 1))
			if cost["count"] != count {
				copy := nbt.ShallowCopyCompound(recipe[key].(nbt.CompoundTag))
				copy.Set("count", count)
				updated[key] = copy
			}
		}
	}
	writeOfferFields(updated, offer)
	return updated
}

// writeOfferFields removes keys too, since a saved value can return to its default.
// vanilla: MerchantOffer.CODEC
func writeOfferFields(recipe nbt.Compound, offer Offer) {
	reward := nbt.Byte(0)
	if offer.RewardExp {
		reward = 1
	}
	for _, field := range [...]struct {
		key        string
		value, def nbt.Tag
	}{
		{"uses", nbt.Int(offer.Uses), nbt.Int(0)},
		{"maxUses", nbt.Int(offer.MaxUses), nbt.Int(4)},
		{"rewardExp", reward, nbt.Byte(1)},
		{"specialPrice", nbt.Int(offer.SpecialPrice), nbt.Int(0)},
		{"demand", nbt.Int(offer.Demand), nbt.Int(0)},
		{"priceMultiplier", nbt.Float(offer.PriceMultiplier), nbt.Float(0)},
		{"xp", nbt.Int(offer.XP), nbt.Int(1)},
	} {
		if field.value == field.def {
			delete(recipe, field.key)
		} else {
			recipe[field.key] = field.value
		}
	}
}
