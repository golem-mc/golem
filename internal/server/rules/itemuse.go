package rules

import (
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/registry"
)

// DefaultUseIsInstant tests Item.getUseDuration; items overriding that method
// (bows, tridents, bundles, spyglasses and instruments) have their own use paths.
// vanilla: Item.getUseDuration, ItemStack.use
func DefaultUseIsInstant(stack protocol.ItemStack) bool {
	if use, ok := registry.StackConsumable(stack); ok {
		return ConsumeTicks(use.Seconds) <= 0
	}
	_, blocks := registry.StackBlocksAttacks(stack)
	return !blocks && registry.StackKineticWeapon(stack) == nil
}
