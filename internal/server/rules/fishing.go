package rules

import (
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/registry"
)

// FishingLootTable is the table an angler's catch rolls.
const FishingLootTable = "minecraft:gameplay/fishing"

// FishingLuck is the rod's bonus loot luck.
// vanilla: EnchantmentHelper.getFishingLuckBonus
func FishingLuck(rod protocol.ItemStack, ctx ...*EnchContext) int {
	value := itemEnchValue(rod, ctx, func(e *registry.EnchantmentEffects) []registry.EnchConditional { return e.FishingLuckBonus }, 0)
	return max(0, int(value))
}

// FishingLureTicks is the rod's fishing-time reduction in ticks.
// vanilla: FishingRodItem.use, EnchantmentHelper.getFishingTimeReduction
func FishingLureTicks(rod protocol.ItemStack, ctx ...*EnchContext) int {
	value := itemEnchValue(rod, ctx, func(e *registry.EnchantmentEffects) []registry.EnchConditional { return e.FishingTimeReduction }, 0)
	return int(float32(max(0, value)) * 20)
}
