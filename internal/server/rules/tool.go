package rules

import (
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/registry"
)

// The digging rules read a held stack's own minecraft:tool when it carries
// one and the item's default otherwise, since a stack's component replaces
// the item's.
// vanilla: ItemStack.getDestroySpeed, ItemStack.isCorrectToolForDrops

// StackMiningSpeed is how fast the stack mines a block before enchantments
// and effects.
// vanilla: Tool.getMiningSpeed
func StackMiningSpeed(stack protocol.ItemStack, b registry.Block) float32 {
	if stack.Removes(protocol.DataComponentTool) {
		return 1
	}
	tool, ok := stack.ToolOf()
	if !ok {
		return registry.MiningSpeed(stack.ItemID, b)
	}
	for _, rule := range tool.Rules {
		if rule.HasSpeed && toolRuleCovers(rule, b) {
			return rule.Speed
		}
	}
	return tool.DefaultMiningSpeed
}

// StackCorrectForDrops reports whether the stack is the correct tool for a
// block's drops.
// vanilla: Tool.isCorrectForDrops
func StackCorrectForDrops(stack protocol.ItemStack, b registry.Block) bool {
	if stack.Removes(protocol.DataComponentTool) {
		return false
	}
	tool, ok := stack.ToolOf()
	if !ok {
		return registry.CorrectForDrops(stack.ItemID, b)
	}
	for _, rule := range tool.Rules {
		if rule.HasCorrect && toolRuleCovers(rule, b) {
			return rule.Correct
		}
	}
	return false
}

// StackCanDestroyInCreative reports whether a creative player may break
// blocks with the stack held.
// vanilla: Item.canDestroyBlock
func StackCanDestroyInCreative(stack protocol.ItemStack) bool {
	if stack.Removes(protocol.DataComponentTool) {
		return true
	}
	if tool, ok := stack.ToolOf(); ok {
		return tool.CanDestroyBlocksInCreative
	}
	return registry.CanDestroyBlocksInCreative(stack.ItemID)
}

// StackIsTool reports whether the stack carries a tool component, which
// Item.mineBlock answers true for whatever the block.
// vanilla: Item.mineBlock
func StackIsTool(stack protocol.ItemStack) bool {
	if stack.Removes(protocol.DataComponentTool) {
		return false
	}
	if _, ok := stack.ToolOf(); ok {
		return true
	}
	return registry.HasTool(stack.ItemID)
}

// StackDamagePerBlock is the wear breaking one block adds to the stack.
// vanilla: Item.mineBlock
func StackDamagePerBlock(stack protocol.ItemStack) int32 {
	if stack.Removes(protocol.DataComponentTool) {
		return 0
	}
	if tool, ok := stack.ToolOf(); ok {
		return tool.DamagePerBlock
	}
	return registry.DamagePerBlock(stack.ItemID)
}

func toolRuleCovers(rule protocol.ToolRule, b registry.Block) bool {
	return blockSetMatchesBlock(rule.Blocks, b)
}
