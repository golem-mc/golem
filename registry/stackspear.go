package registry

import "github.com/golem-mc/golem/internal/protocol"

// StackKineticWeapon returns the stack's effective kinetic_weapon: its own,
// else the item's, nil when neither or the patch removes it.
// vanilla: ItemStack.get(DataComponents.KINETIC_WEAPON)
func StackKineticWeapon(stack protocol.ItemStack) *KineticWeapon {
	if stack.Empty() {
		return nil
	}
	if len(stack.Raw) != 0 {
		if v, ok := stack.RecordOf(protocol.DataComponentKineticWeapon); ok {
			k := &KineticWeapon{DamageMultiplier: v[6].(float32)}
			k.ContactCooldown, k.Delay = v[0].(int32), v[1].(int32)
			k.Dismount, k.Knockback, k.Damage = kineticConditionOf(v[2]), kineticConditionOf(v[3]), kineticConditionOf(v[4])
			k.Sound, k.HitSound = soundOfRecord(v[7]), soundOfRecord(v[8])
			return k
		}
	}
	if stack.Removes(protocol.DataComponentKineticWeapon) {
		return nil
	}
	return KineticWeaponOf(stack.ItemID)
}

// StackPiercingWeapon returns the stack's effective piercing_weapon: its own,
// else the item's, false when neither or the patch removes it.
// vanilla: ItemStack.get(DataComponents.PIERCING_WEAPON)
func StackPiercingWeapon(stack protocol.ItemStack) (PiercingWeapon, bool) {
	if stack.Empty() {
		return PiercingWeapon{}, false
	}
	if len(stack.Raw) != 0 {
		if v, ok := stack.RecordOf(protocol.DataComponentPiercingWeapon); ok {
			return PiercingWeapon{
				DealsKnockback: v[0].(bool), Dismounts: v[1].(bool),
				Sound: soundOfRecord(v[2]), HitSound: soundOfRecord(v[3]),
			}, true
		}
	}
	if stack.Removes(protocol.DataComponentPiercingWeapon) {
		return PiercingWeapon{}, false
	}
	return PiercingWeaponOf(stack.ItemID)
}

func kineticConditionOf(v any) *KineticCondition {
	f, ok := v.([]any)
	if !ok {
		return nil
	}
	return &KineticCondition{MaxDuration: f[0].(int32), MinSpeed: f[1].(float32), MinRelativeSpeed: f[2].(float32)}
}

// soundOfRecord maps a record's sound holder to a registered sound; an
// absent or unregistered one plays nothing.
func soundOfRecord(v any) Sound {
	h, ok := v.(protocol.SoundHolder)
	if !ok {
		return SoundNone
	}
	if h.Name != "" {
		id, ok := SoundEventID(h.Name)
		if !ok {
			return SoundNone
		}
		return Sound(id + 1)
	}
	if h.ID < 0 || int(h.ID) >= len(SoundEvents) {
		return SoundNone
	}
	return Sound(h.ID + 1)
}
