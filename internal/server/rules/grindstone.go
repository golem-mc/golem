package rules

import (
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/registry"
)

// The grindstone's maths: the result slot and what taking it pays.
// vanilla: GrindstoneMenu.createResult, computeResult, mergeItems, and the
// result slot's onTake

// GrindstoneCombine is the result for the two inputs: nothing for two
// empties or a stack over one; a lone enchanted item without its non-curse
// enchantments; two single same items merged with their remaining
// durability summed plus a twentieth (two unbreakable ones as a stack of
// two). Only curses survive.
// vanilla: GrindstoneMenu.createResult
func GrindstoneCombine(input, additional protocol.ItemStack) protocol.ItemStack {
	switch {
	case input.Empty() && additional.Empty():
		return protocol.ItemStack{}
	case input.Count > 1 || additional.Count > 1:
		return protocol.ItemStack{}
	case input.Empty() || additional.Empty():
		lone := additional
		if !input.Empty() {
			lone = input
		}
		if len(lone.Enchantments) == 0 && len(lone.StoredEnchantments) == 0 {
			// The lone item without magic has nothing to show.
			// vanilla: EnchantmentHelper.hasAnyEnchantments
			return protocol.ItemStack{}
		}
		// The strip writes into the stack it is handed, and its maps
		// alias the caller's: a lone input answers with copies of them.
		lone.Enchantments = cloneEnchantments(lone.Enchantments)
		lone.StoredEnchantments = cloneEnchantments(lone.StoredEnchantments)
		return grindstoneStripped(lone)
	}
	if input.ItemID != additional.ItemID {
		// Two of different kinds are nothing to make of.
		return protocol.ItemStack{}
	}
	return grindstoneMerge(input, additional)
}

// grindstoneStripped removes non-curse enchantments, resets the repair cost
// to one raise per kept enchantment, and turns a bare enchanted book back
// into a book.
// vanilla: GrindstoneMenu.removeNonCursesFrom
func grindstoneStripped(copy protocol.ItemStack) protocol.ItemStack {
	kept := Disenchant(&copy)
	if isEnchantedBook(copy) && kept == 0 {
		copy.ItemID = int32(registry.ItemBook)
	}
	cost := int32(0)
	for range kept {
		cost = IncreasedRepairCost(cost)
	}
	// The cost starts over with the curses kept; what the inputs had
	// paid before is gone with their magic.
	copy.SetRepairCost(cost)
	return copy
}

// grindstoneMerge merges two single same items: remaining durability summed
// plus a twentieth of the larger max, which the result keeps. An item that
// neither wears nor breaks pairs only with its twin, as a stack of two.
// vanilla: GrindstoneMenu.mergeItems, ItemStack.isDamageableItem, ItemStack.matches
func grindstoneMerge(input, additional protocol.ItemStack) protocol.ItemStack {
	count := int32(1)
	damageable := registry.StackDamageable(input)
	if !damageable {
		if registry.StackMaxStackSize(input) < 2 || !input.Equal(additional) {
			return protocol.ItemStack{}
		}
		count = 2
	}
	merged := input
	merged.Count = count
	// The curse merge writes into the stack it is handed, and its maps
	// alias the caller's: the merge answers with copies of them.
	merged.Enchantments = cloneEnchantments(input.Enchantments)
	merged.StoredEnchantments = cloneEnchantments(input.StoredEnchantments)
	if damageable {
		inputMax, additionalMax := registry.StackMaxDamage(input), registry.StackMaxDamage(additional)
		maxDamage := max(inputMax, additionalMax)
		remaining := (inputMax - input.Damage) + (additionalMax - additional.Damage) + maxDamage*5/100
		registry.SetStackMaxDamage(&merged, maxDamage)
		merged.SetDamage(max(maxDamage-remaining, 0))
	}
	grindstoneMergeCurses(&merged, additional)
	return grindstoneStripped(merged)
}

// grindstoneMergeCurses upgrades the additional's curses into the item's
// component (the strip after removes the rest), so each curse survives at
// the higher level; one the item has keeps its own.
// vanilla: GrindstoneMenu.mergeEnchantsFrom, ItemEnchantments.upgrade
func grindstoneMergeCurses(item *protocol.ItemStack, additional protocol.ItemStack) {
	component := enchantmentComponent(item)
	own := *component
	extra := anvilEnchantmentsOf(additional)
	for id, level := range extra {
		if level <= 0 {
			continue
		}
		if level > 255 {
			level = 255
		}
		if registry.EnchantmentInTag(id, "minecraft:curse") && own[id] != 0 {
			continue
		}
		if own == nil {
			own = make(map[int32]int32, len(extra))
		}
		if level > own[id] {
			own[id] = level
		}
	}
	item.SetEnchantments(isEnchantedBook(*item), own)
}

// GrindstoneWorth is the experience the stack's removed magic is worth:
// each non-curse enchantment's minimum cost at its level, summed, over the
// component the stack's own kind keeps its levels in.
// vanilla: GrindstoneMenu.getExperienceFromItem, Enchantment.getMinCost
func GrindstoneWorth(stack protocol.ItemStack) int {
	worth := 0
	for id, level := range anvilEnchantmentsOf(stack) {
		def, ok := registry.EnchantmentEffectsByID(id)
		if !ok || level <= 0 || registry.EnchantmentInTag(id, "minecraft:curse") {
			continue
		}
		worth += int(def.MinCost(level))
	}
	return worth
}

// GrindstoneAward is the orbs for worth: half rounded up, plus as many more
// points as roll picks below it. Zero worth pays nothing, the sound still
// playing.
// vanilla: GrindstoneMenu.getExperienceAmount
func GrindstoneAward(worth int, roll float64) int {
	if worth <= 0 {
		return 0
	}
	half := (worth + 1) / 2
	return half + int(roll*float64(half))
}
