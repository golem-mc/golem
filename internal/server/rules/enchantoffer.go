package rules

import (
	"math"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/registry"
)

// The enchanting table's offers: a pure function of the player's enchantment
// seed, the bookshelves in reach and the item, every roll through the
// java.util.Random a Level keeps, so the offers match vanilla draw for draw.
// Vanilla re-seeds before each computation (the cost sweep from the seed,
// each slot's list from seed plus slot), so no RandomSource drift needs
// carrying.
// vanilla: EnchantmentMenu.slotsChanged, clickMenuButton,
// EnchantmentHelper.getEnchantmentCost, getAvailableEnchantmentResults,
// selectEnchantment

// EnchOffer is one enchantment the table's maths picked: the registry ID
// the window's hover properties name it by, and the level it lands at.
type EnchOffer struct {
	Enchantment int32
	Level       int32
}

// itemEnchantable is the enchantability of what a stack holds, the
// minecraft:enchantable component's value. It is the one thing both halves
// of the maths ask the stack for.
func itemEnchantable(stack protocol.ItemStack) int {
	return int(registry.StackEnchantable(stack))
}

// isPlainBook reports the plain book, which takes every enchantment the table
// draws from and loses one member of a multi-enchantment list.
func isPlainBook(stack protocol.ItemStack) bool {
	return registry.Item(stack.ItemID) == registry.ItemBook
}

// EnchantmentOfferCost is slot's starting cost, drawn on r and scaled by the
// counted bookshelves (capped at 15 here) and enchantability.
// vanilla: EnchantmentHelper.getEnchantmentCost
func EnchantmentOfferCost(r *JavaRandom, slot, bookcases, enchantable int) int {
	if enchantable == 0 {
		return 0
	}
	if bookcases > 15 {
		bookcases = 15
	}
	selected := int(r.NextIntBound(8)) + 1 + (bookcases >> 1) + int(r.NextIntBound(int32(bookcases)+1))
	switch slot {
	case 0:
		// The top offer is the meanest of the three.
		return max(selected/3, 1)
	case 1:
		return selected*2/3 + 1
	default:
		return max(selected, bookcases*2)
	}
}

// enchantmentPrimary reports whether the enchantment may land on the item:
// in its supported tag, and in its primary tag or the data has none. A plain
// book always may.
// vanilla: Enchantment.isPrimaryItem
func enchantmentPrimary(def *registry.EnchantmentDef, itemID int32, isBook bool) bool {
	if isBook {
		return true
	}
	return def.Supports(registry.Item(itemID)) && def.PrimaryFor(registry.Item(itemID))
}

// enchantmentCompatible reports whether two different enchantments may share
// an item: neither in the other's exclusive-set tag, either direction
// deciding.
// vanilla: Enchantment.areCompatible
func enchantmentCompatible(a, b EnchOffer) bool {
	if a.Enchantment == b.Enchantment {
		return false
	}
	if def, ok := registry.EnchantmentEffectsByID(a.Enchantment); ok && def.ExcludesWith(b.Enchantment) {
		return false
	}
	if def, ok := registry.EnchantmentEffectsByID(b.Enchantment); ok && def.ExcludesWith(a.Enchantment) {
		return false
	}
	return true
}

// CanEnchant reports whether the enchantment may go on the stack: the item is
// supported and it is compatible with every enchantment already there (a
// book's stored ones).
// vanilla: Enchantment.canEnchant, EnchantmentHelper.isEnchantmentCompatible
func CanEnchant(stack protocol.ItemStack, id int32) bool {
	def, ok := registry.EnchantmentEffectsByID(id)
	if !ok {
		return false
	}
	if !def.Supports(registry.Item(stack.ItemID)) {
		return false
	}
	existing := stack.Enchantments
	if isEnchantedBook(stack) {
		existing = stack.StoredEnchantments
	}
	for other := range existing {
		if !enchantmentCompatible(EnchOffer{Enchantment: id}, EnchOffer{Enchantment: other}) {
			return false
		}
	}
	return true
}

// enchAvailable is one of the enchantments a worth could land on, with its
// weight alongside: the weighted draw walks both together.
type enchAvailable struct {
	offer  EnchOffer
	weight int
}

// enchantmentsAvailable is, for each candidate in pool order, the highest
// level whose cost window holds cost (after select's rounding). candidates is
// the table's tag or another's (a loot function's on_random_loot) as IDs.
// vanilla: EnchantmentHelper.getAvailableEnchantmentResults
func enchantmentsAvailable(cost int, stack protocol.ItemStack, candidates []int32) []enchAvailable {
	itemID := stack.ItemID
	isBook := isPlainBook(stack)
	var offers []enchAvailable
	for _, id := range candidates {
		def, ok := registry.EnchantmentEffectsByID(id)
		if !ok {
			continue
		}
		if !enchantmentPrimary(def, itemID, isBook) {
			continue
		}
		for level := def.MaxLevel; level >= 1; level-- {
			if cost < int(def.MinCost(level)) || cost > int(def.MaxCost(level)) {
				continue
			}
			offers = append(offers, enchAvailable{offer: EnchOffer{Enchantment: id, Level: level}, weight: int(def.Weight)})
			break
		}
	}
	return offers
}

// EnchantRandom is the draws SelectEnchantment makes: *JavaRandom, or a
// worldgen random through an adapter.
type EnchantRandom interface {
	NextIntBound(bound int32) int32
	NextFloat() float32
}

// weightedPick draws by weight in list order, the first whose share holds
// the draw winning; nothing for an empty list or zero total.
// vanilla: WeightedRandom.getRandomItem
func weightedPick(r EnchantRandom, offers []enchAvailable) (EnchOffer, bool) {
	total := 0
	for _, o := range offers {
		total += o.weight
	}
	if total <= 0 {
		return EnchOffer{}, false
	}
	selection := int(r.NextIntBound(int32(total)))
	for _, o := range offers {
		selection -= o.weight
		if selection < 0 {
			return o.offer, true
		}
	}
	return EnchOffer{}, false
}

// SelectEnchantment is the enchantments one offer of cost puts on the item,
// drawn on the caller-seeded r: the cost is bent by enchantability and float
// rounding, then fitting enchantments are drawn by weight, each further draw
// halving the cost until the one-in-fifty check stops it. candidates is the
// pool (the table's tag or a loot function's).
// vanilla: EnchantmentHelper.selectEnchantment
func SelectEnchantment(r EnchantRandom, stack protocol.ItemStack, cost int, candidates []int32) []EnchOffer {
	enchantable := itemEnchantable(stack)
	if enchantable == 0 {
		return nil
	}
	// The worth bends by one plus two draws over the enchantability, then
	// by the span a pair of float draws makes around it, all in the
	// float32 the vanilla arithmetic runs in.
	cost += 1 + int(r.NextIntBound(int32(enchantable/4+1))) + int(r.NextIntBound(int32(enchantable/4+1)))
	span := (r.NextFloat() + r.NextFloat() - 1.0) * 0.15
	bent := float32(cost) + float32(cost)*span
	cost = max(int(math.Floor(float64(bent)+0.5)), 1)

	offers := enchantmentsAvailable(cost, stack, candidates)
	if len(offers) == 0 {
		return nil
	}
	var results []EnchOffer
	if pick, ok := weightedPick(r, offers); ok {
		results = append(results, pick)
	}
	for int(r.NextIntBound(50)) <= cost {
		if len(results) > 0 {
			last := results[len(results)-1]
			kept := offers[:0]
			for _, o := range offers {
				if enchantmentCompatible(last, o.offer) {
					kept = append(kept, o)
				}
			}
			offers = kept
		}
		if len(offers) == 0 {
			break
		}
		if pick, ok := weightedPick(r, offers); ok {
			results = append(results, pick)
		}
		cost /= 2
	}
	return results
}

// EnchantList is slot's enchantments, re-seeded from seed plus slot so the
// same seed and item always give the same list. A plain book's list over one
// loses a member, keeping some mystery in a wide offer.
// vanilla: EnchantmentMenu.getEnchantmentList
func EnchantList(r *JavaRandom, seed int32, slot int, stack protocol.ItemStack, cost int) []EnchOffer {
	r.SetSeed(int64(seed) + int64(slot))
	list := SelectEnchantment(r, stack, cost, registry.EnchantingTableIDs())
	if isPlainBook(stack) && len(list) > 1 {
		drop := int(r.NextIntBound(int32(len(list))))
		list = append(list[:drop], list[drop+1:]...)
	}
	return list
}

// EnchantOffers is what a table shows: the three costs drawn in one sweep
// from the seed; each showable slot's list from seed plus slot; and the
// hovered enchantment drawn from the state the list left. A cost below the
// slot's number is no offer, and its slots carry the client's -1 dashes.
//
// A missing or unenchantable item (no enchantability, or already
// enchanted) leaves everything at defaults without a draw, as vanilla's
// early exit does.
// vanilla: EnchantmentMenu.slotsChanged
func EnchantOffers(seed int32, bookcases int, stack protocol.ItemStack) (costs, clues, levels [3]int32) {
	if stack.Empty() || !stackEnchantable(stack) {
		return [3]int32{}, [3]int32{-1, -1, -1}, [3]int32{-1, -1, -1}
	}
	r := NewJavaRandom(int64(seed))
	for i := range costs {
		c := EnchantmentOfferCost(r, i, bookcases, itemEnchantable(stack))
		clues[i] = -1
		levels[i] = -1
		if c < i+1 {
			continue
		}
		costs[i] = int32(c)
	}
	for i := range costs {
		if costs[i] <= 0 {
			continue
		}
		list := EnchantList(r, seed, i, stack, int(costs[i]))
		if len(list) == 0 {
			continue
		}
		pick := int(r.NextIntBound(int32(len(list))))
		clues[i] = list[pick].Enchantment
		levels[i] = list[pick].Level
	}
	return costs, clues, levels
}

// stackEnchantable reports an enchantable component and no enchantments yet:
// the table makes enchantments, it does not add to them.
// vanilla: ItemStack.isEnchantable
func stackEnchantable(stack protocol.ItemStack) bool {
	return itemEnchantable(stack) != 0 && !stack.Removes(protocol.DataComponentEnchantments) && len(stack.Enchantments) == 0
}
