package registry

import "github.com/golem-mc/golem/internal/protocol"

// StackDyeColour returns the DyeNames index of the stack's dye component: its
// own value, none when the patch removes it, else the item's default. The
// dye tags still decide which items qualify; this decides the colour.
// vanilla: DataComponents.DYE
func StackDyeColour(stack protocol.ItemStack) (int32, bool) {
	if stack.Empty() || stack.Removed != 0 && stack.Removes(protocol.DataComponentDye) {
		return 0, false
	}
	if len(stack.Raw) != 0 {
		if id, _, ok := StackVariant(stack, protocol.DataComponentDye); ok {
			return id, true
		}
	}
	return Item(stack.ItemID).DyeColour()
}
