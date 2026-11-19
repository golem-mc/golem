package rules

import (
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/registry"
)

// Item wear and breaking: every durability cost (a tool's block, a weapon's
// hit, armour's turned blow) comes through here, damaged per the item's
// components and ended by its max damage.

// damageable is ItemStack.isDamageableItem: a stack of an item carrying a max
// damage of its own that is not unbreakable.
// vanilla: ItemStack.isDamageableItem
func damageable(stack protocol.ItemStack) bool {
	return registry.StackMaxDamage(stack) > 0 && !stack.Unbreakable
}

// Wear adds n to the stack's wear, returning it empty and broke when the wear
// reaches max damage (vanilla removes the item rather than leaving it one use
// past). An empty stack, one without max damage, or an unbreakable one is
// returned unchanged.
func Wear(stack protocol.ItemStack, n int32) (after protocol.ItemStack, broke bool) {
	if n <= 0 || stack.Damage < 0 || !damageable(stack) {
		return stack, false
	}
	max := registry.StackMaxDamage(stack)
	if stack.Damage+n >= max {
		return protocol.ItemStack{}, true
	}
	stack.Damage += n
	return stack, false
}

// NextWearBreaks reports whether one more point of wear ends the stack, the
// gate a nearly spent trident's charge and release both check.
// vanilla: ItemStack.nextDamageWillBreak
func NextWearBreaks(stack protocol.ItemStack) bool {
	return damageable(stack) && stack.Damage >= registry.StackMaxDamage(stack)-1
}

// BreakStatus is the equipment-break Entity Event status for an inventory
// slot; the held hotbar slot is the main hand. Other slots have none, as
// wear lands only on hands and armour.
func BreakStatus(slot int) (uint8, bool) {
	switch {
	case slot >= player.HotbarSlot && slot < player.HotbarSlot+player.HotbarSlots:
		return protocol.EntityStatusBreakMainHand, true
	case slot == player.OffHandSlot:
		return protocol.EntityStatusBreakOffHand, true
	case slot == player.ArmorSlot:
		return protocol.EntityStatusBreakHead, true
	case slot == player.ArmorSlot+1:
		return protocol.EntityStatusBreakChest, true
	case slot == player.ArmorSlot+2:
		return protocol.EntityStatusBreakLegs, true
	case slot == player.ArmorSlot+3:
		return protocol.EntityStatusBreakFeet, true
	}
	return 0, false
}
