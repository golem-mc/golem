package registry

import (
	"github.com/golem-mc/golem/internal/protocol"
)

// StackDamageType returns the damage type the stack's blows deal: its own
// damage_type, else the item's, 0 when neither or the patch removes it.
// vanilla: ItemStack.getDamageSource
func StackDamageType(stack protocol.ItemStack) DamageType {
	if data, ok := propBytes(stack, protocol.DataComponentDamageType); ok {
		r := protocol.NewReader(data)
		id := r.ReadVarInt()
		if d := DamageType(id + 1); r.Err() == nil && id >= 0 && d.Name() != "" {
			return d
		}
	}
	if stack.Removes(protocol.DataComponentDamageType) {
		return 0
	}
	return ItemDamageType(stack.ItemID)
}

// StackMinimumAttackCharge returns the stack's own minimum_attack_charge, 0
// when the patch removes it, else the item's.
// vanilla: Player.cannotAttackWithItem
func StackMinimumAttackCharge(stack protocol.ItemStack) float32 {
	if data, ok := propBytes(stack, protocol.DataComponentMinimumAttackCharge); ok {
		r := protocol.NewReader(data)
		if v := r.ReadF32(); r.Err() == nil {
			return v
		}
	}
	if stack.Removes(protocol.DataComponentMinimumAttackCharge) {
		return 0
	}
	return MinimumAttackCharge(stack.ItemID)
}
