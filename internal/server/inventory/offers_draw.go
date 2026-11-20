package inventory

import (
	"maps"
	"math/rand"
	"slices"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// DrawOffers draws a trade set's offers for a villager of the type onto the
// end of a saved Offers compound, returning a copy. A drawn trade that
// yields no offer leaves the draw as vanilla's does: it is dropped from the
// pool (with duplicates) or already out of it.
// vanilla: AbstractVillager.addOffersFromItemListings,
// AbstractVillager.addOffersFromItemListingsWithoutDuplicates
func DrawOffers(tag nbt.Compound, set registry.TradeSet, villagerType string, rng *rand.Rand, ctx *rules.LootContext) nbt.Compound {
	pool := slices.Clone(set.Trades)
	var drawn []nbt.Tag
	for len(drawn) < set.Amount && len(pool) > 0 {
		i := rng.Intn(len(pool))
		made := offerOf(pool[i], villagerType, rng, &drawn, ctx)
		if !set.Duplicates || !made {
			pool = slices.Delete(pool, i, i+1)
		}
	}
	old, _ := nbt.Get[nbt.List](tag, "Recipes")
	out := make(nbt.Compound, len(tag)+1)
	maps.Copy(out, tag)
	out["Recipes"] = nbt.List{Elem: nbt.TypeCompound, Items: append(slices.Clone(old.Items), drawn...)}
	return out
}

// offerOf appends the trade's saved recipe and reports whether it made one.
// vanilla: VillagerTrade.getOffer
func offerOf(t registry.VillagerTrade, villagerType string, rng *rand.Rand, out *[]nbt.Tag, ctx *rules.LootContext) bool {
	if t.Unsupported || len(t.Variants) > 0 && !slices.Contains(t.Variants, villagerType) {
		return false
	}
	var stack protocol.ItemStack
	if t.GivesStack != nil {
		// Component writers replace shared values; Raw also needs its own backing array.
		stack = *t.GivesStack
		stack.Raw = slices.Clone(stack.Raw)
	} else {
		id, ok := registry.ItemID(t.Gives.Item)
		if !ok || t.Gives.Count < 1 {
			return false
		}
		stack = protocol.ItemStack{ItemID: id, Count: t.Gives.Count}
	}
	if !registry.ValidStackTemplate(stack) {
		return false
	}
	result, additional := rules.ApplyTradeModifiers(t.Modifiers, stack, rng, ctx)
	if result.Empty() {
		return false
	}
	for id := range result.StoredEnchantments {
		name, _ := registry.EnchantmentName(id)
		if slices.Contains(t.DoublePrice, name) || t.DoublePriceTag != "" && registry.EnchantmentInTag(id, t.DoublePriceTag) {
			additional *= 2
			break
		}
	}
	cost := t.Wants
	cost.Count += additional
	wants, ok := costTag(cost)
	if !ok {
		return false
	}
	recipe := nbt.Compound{
		"buy":  wants,
		"sell": registry.StackTag(result),
	}
	writeOfferFields(recipe, Offer{
		MaxUses: max(t.MaxUses, 1), XP: max(t.XP, 0),
		PriceMultiplier: max(t.Discount, 0), RewardExp: true,
	})
	if t.Extra.Count > 0 {
		extra, ok := costTag(t.Extra)
		if !ok {
			return false
		}
		recipe["buyB"] = extra
	}
	*out = append(*out, recipe)
	return true
}

// costTag writes an ItemCost with its count clamped to one stack; a count
// under one makes no offer.
// vanilla: TradeCost.toItemCost
func costTag(c registry.TradeCost) (nbt.Compound, bool) {
	id, ok := registry.ItemID(c.Item)
	count := min(c.Count, registry.MaxStackSize(id))
	if !ok || count < 1 {
		return nil, false
	}
	tag := nbt.Compound{"id": nbt.String(c.Item), "count": nbt.Int(count)}
	if c.Water {
		tag["components"] = nbt.Compound{"minecraft:potion_contents": nbt.Compound{"potion": nbt.String("minecraft:water")}}
	}
	return tag, true
}
