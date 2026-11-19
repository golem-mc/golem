package rules

import (
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/registry"
)

// EnchantByProvider enchants the stack by the provider, raising levels it
// already has. special is the local difficulty's special multiplier.
// vanilla: EnchantmentProvider.enchant, EnchantmentHelper.enchantItemFromProvider
func EnchantByProvider(p registry.EnchantmentProvider, r EnchantRandom, stack *protocol.ItemStack, special float32) {
	draw := func(v registry.IntRange) int {
		if v.Max <= v.Min {
			return int(v.Min)
		}
		return int(v.Min + r.NextIntBound(v.Max-v.Min+1))
	}
	raise := func(id, level int32) {
		stack.SetEnchantment(false, id, max(stack.Enchantments[id], level))
	}
	switch p.Kind {
	case registry.EnchProviderSingle:
		if len(p.Names) == 0 {
			return
		}
		id, ok := registry.EnchantmentID(p.Names[0])
		if !ok {
			return
		}
		level := int32(draw(p.Level))
		if def, ok := registry.EnchantmentEffectsByID(id); ok {
			level = min(max(level, 1), max(def.MaxLevel, 1))
		}
		raise(id, level)
	case registry.EnchProviderByCost, registry.EnchProviderByCostWithDifficulty:
		var cost int
		if p.Kind == registry.EnchProviderByCost {
			cost = draw(p.Cost)
		} else {
			// Always a draw, even for a span of 0.
			cost = int(p.MinCost + r.NextIntBound(int32(special*float32(p.MaxCostSpan))+1))
		}
		for _, offer := range SelectEnchantment(r, *stack, cost, p.Candidates()) {
			raise(offer.Enchantment, offer.Level)
		}
	}
}
