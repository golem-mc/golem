package registry

import "github.com/golem-mc/golem/internal/protocol"

// ownRecord returns the stack's own record component, removed reporting that
// its patch removes the component.
func ownRecord(stack protocol.ItemStack, component int32) (values []any, own, removed bool) {
	if len(stack.Raw) != 0 {
		if values, ok := stack.RecordOf(component); ok {
			return values, true, false
		}
	}
	return nil, false, stack.Removed != 0 && stack.Removes(component)
}

// StackInteractVibrations reports whether a use of the stack posts its
// item_interact game events: its own use_effects, none when the patch removes
// them, else the item's.
// vanilla: ItemStack.causeUseVibration
func StackInteractVibrations(stack protocol.ItemStack) bool {
	if values, own, removed := ownRecord(stack, protocol.DataComponentUseEffects); own {
		return values[1].(bool)
	} else if removed {
		return false
	}
	return InteractVibrations(stack.ItemID)
}

// StackAttackAnimation returns the swing the stack attacks with: its own
// attack_animation, else the item's.
// vanilla: ItemStack.getAttackAnimation
func StackAttackAnimation(stack protocol.ItemStack) SwingAnimation {
	if values, own, removed := ownRecord(stack, protocol.DataComponentAttackAnimation); own {
		return SwingAnimation{Type: values[0].(int32), Duration: values[1].(int32)}
	} else if removed {
		return DefaultSwingAnimation
	}
	return AttackAnimationOf(stack.ItemID)
}

// StackInteractAnimation returns the swing the stack interacts with: its own
// interact_animation, else the default.
// vanilla: ItemStack.getInteractAnimation
func StackInteractAnimation(stack protocol.ItemStack) SwingAnimation {
	if values, own, _ := ownRecord(stack, protocol.DataComponentInteractAnimation); own {
		return SwingAnimation{Type: values[0].(int32), Duration: values[1].(int32)}
	}
	return DefaultSwingAnimation
}
