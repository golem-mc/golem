package registry

import (
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/nbt"
)

// StackMaxStackSize returns how many of the stack's item one slot holds: its
// own max_stack_size, 1 when the patch removes it, else the item's.
// vanilla: ItemStack.getMaxStackSize
func StackMaxStackSize(stack protocol.ItemStack) int32 {
	if len(stack.Raw) != 0 {
		if n, ok := stack.MaxStackSizeOf(); ok {
			return n
		}
	}
	if stack.Removes(protocol.DataComponentMaxStackSize) {
		return 1
	}
	return MaxStackSize(stack.ItemID)
}

func withMaxStackSizeTag(components nbt.Compound, stack protocol.ItemStack) nbt.Compound {
	n, ok := stack.MaxStackSizeOf()
	if !ok {
		return components
	}
	if components == nil {
		components = nbt.Compound{}
	}
	components["minecraft:max_stack_size"] = nbt.Int(n)
	return components
}

// StackComponentsValid reports whether the stack is not both damageable and
// stackable.
// vanilla: ItemStack.validateComponents
func StackComponentsValid(stack protocol.ItemStack) bool {
	return StackMaxDamage(stack) <= 0 || StackMaxStackSize(stack) <= 1
}

// vanilla: DataComponents.MAX_STACK_SIZE
func readMaxStackSizeComponent(stack *protocol.ItemStack, value nbt.Tag) bool {
	size, ok := nbt.IntValue(value)
	if !ok || size < protocol.MinMaxStackSize || size > protocol.MaxMaxStackSize {
		return false
	}
	if size == MaxStackSize(stack.ItemID) {
		stack.ResetComponent(protocol.DataComponentMaxStackSize)
		return true
	}
	stack.SetMaxStackSize(size)
	return true
}
