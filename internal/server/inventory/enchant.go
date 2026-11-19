package inventory

import (
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/registry"
)

// The enchanting table: Menu[0] the item, Menu[1] the lapis, and ten window
// properties carrying the offers, drawn from the player's seed, the shelves
// and the item through internal/server/rules' java.util.Random port. A press spends
// lapis and levels and rerolls the seed; the server sends properties, sound
// and corrections.
// vanilla: EnchantmentMenu

// EnchantStartProps are the properties a table's window opens with: no
// costs, the player's seed, and the hovers hidden (-1).
func EnchantStartProps(seed int32) [protocol.EnchantPropertyCount]int32 {
	var props [protocol.EnchantPropertyCount]int32
	props[protocol.EnchantPropertySeed] = seed
	props[protocol.EnchantPropertyEnchant0] = -1
	props[protocol.EnchantPropertyEnchant1] = -1
	props[protocol.EnchantPropertyEnchant2] = -1
	props[protocol.EnchantPropertyLevel0] = -1
	props[protocol.EnchantPropertyLevel1] = -1
	props[protocol.EnchantPropertyLevel2] = -1
	return props
}

// EnchantProps is the ten properties for a seed, shelf count and item.
func EnchantProps(seed int32, shelves int, item protocol.ItemStack) [protocol.EnchantPropertyCount]int32 {
	costs, clues, levels := rules.EnchantOffers(seed, shelves, item)
	var props [protocol.EnchantPropertyCount]int32
	copy(props[:3], costs[:])
	props[protocol.EnchantPropertySeed] = seed
	copy(props[protocol.EnchantPropertyEnchant0:], clues[:])
	copy(props[protocol.EnchantPropertyLevel0:], levels[:])
	return props
}

// EnchantPlan is a press worked out but not yet applied: the enchantments the
// item gets, the lapis and levels it spends and the offer cost it needs.
type EnchantPlan struct {
	Offers []rules.EnchOffer
	// Cost is the lapis and the levels spent (button + 1).
	Cost int32
	// Required is the offer's level cost, the player's level floor.
	Required int32
}

// PlanEnchant works out a press on button 0-2 without changing menu; false
// when it cannot pay or draws nothing. costs are the offers stored at the
// last slot change, not a fresh shelf count.
// vanilla: EnchantmentMenu.clickMenuButton
func PlanEnchant(menu []protocol.ItemStack, button, seed int32, costs [3]int32, level int32, creative bool) (EnchantPlan, bool) {
	item := menu[0]
	cost := button + 1
	lapis := menu[1]
	if (lapis.Empty() || lapis.Count < cost) && !creative {
		return EnchantPlan{}, false
	}
	if costs[button] <= 0 || item.Empty() {
		return EnchantPlan{}, false
	}
	if !creative && (level < cost || level < costs[button]) {
		return EnchantPlan{}, false
	}
	offers := rules.EnchantList(rules.NewJavaRandom(int64(seed)), seed, int(button),
		item, int(costs[button]))
	if len(offers) == 0 {
		return EnchantPlan{}, false
	}
	return EnchantPlan{Offers: offers, Cost: cost, Required: costs[button]}, true
}

// EnchantPress applies a press on button 0-2: enchants menu[0], spends
// menu[1]'s lapis, and returns the level cost; false, menu untouched, when it
// cannot pay or draws nothing. The caller rerolls the seed only on true.
func EnchantPress(menu []protocol.ItemStack, button, seed int32, costs [3]int32, level int32, creative bool) (int32, bool) {
	plan, ok := PlanEnchant(menu, button, seed, costs, level, creative)
	if !ok || !ApplyEnchant(menu, plan, creative) {
		return 0, false
	}
	return plan.Cost, true
}

// ApplyEnchant enchants menu[0] with plan's offers and spends the lapis;
// false, menu untouched, when the item is gone or the lapis fell short since
// the plan.
func ApplyEnchant(menu []protocol.ItemStack, plan EnchantPlan, creative bool) bool {
	item := menu[0]
	lapis := menu[1]
	cost := plan.Cost
	if item.Empty() || (!creative && (lapis.Empty() || lapis.Count < cost)) {
		return false
	}
	offers := plan.Offers
	// A book becomes an enchanted book with stored enchantments. An offer
	// never lowers a level already set (vanilla: ItemStack.enchant).
	var enchanted protocol.ItemStack
	isBook := false
	if registry.Item(item.ItemID) == registry.ItemBook {
		isBook = true
		// The enchanted book keeps the book's components, a custom name
		// and repair cost among them (vanilla: ItemStack.transmuteCopy).
		enchanted = item
		enchanted.ItemID = int32(registry.ItemEnchantedBook)
	} else {
		enchanted = item
		enchanted.SetEnchantments(false, nil)
		enchanted.SetEnchantments(true, nil)
	}
	for _, offer := range offers {
		levels := enchanted.Enchantments
		if isBook {
			levels = enchanted.StoredEnchantments
		}
		enchanted.SetEnchantment(isBook, offer.Enchantment, max(levels[offer.Enchantment], offer.Level))
	}
	// A creative player's lapis is not spent.
	// vanilla: ItemStack.consume
	if !creative {
		lapis.Count -= cost
		if lapis.Count <= 0 {
			lapis = protocol.ItemStack{}
		}
	}
	menu[0] = enchanted
	menu[1] = lapis
	return true
}
