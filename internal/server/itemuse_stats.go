package server

import (
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/registry"
)

// awardUsedHeld counts one use of the held item; call it before the use
// spends the stack. vanilla: ItemStack.useOn, Item.use
func (c *client) awardUsedHeld(hand int32) {
	c.awardItemStat(registry.StatTypeUsed, c.heldItem(hand).ItemID, 1)
}

// awardWeaponHit counts a landed blow with a weapon; an item that takes no
// wear from a blow is not one. vanilla: ItemStack.hurtEnemy
func (c *client) awardWeaponHit(held protocol.ItemStack) {
	if registry.StackDamagePerAttack(held) > 0 {
		c.awardItemStat(registry.StatTypeUsed, held.ItemID, 1)
	}
}
