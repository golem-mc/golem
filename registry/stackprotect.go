package registry

import (
	"slices"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/nbt"
)

// StackGlider reports whether the stack is a glider: its own glider
// component, else its item's (the elytra's); false when its patch removes it.
// vanilla: ItemStack.has(DataComponents.GLIDER)
func StackGlider(stack protocol.ItemStack) bool {
	return !stack.Removes(protocol.DataComponentGlider) &&
		(stack.HasGlider() || propsOf(stack.ItemID).Glider)
}

// StackDeathProtection returns the stack's death effects: its own
// death_protection, else its item's. False for a stack that saves nobody,
// one whose patch removes it included.
// vanilla: ItemStack.get(DataComponents.DEATH_PROTECTION)
func StackDeathProtection(stack protocol.ItemStack) ([]ConsumeEffect, bool) {
	if stack.Removes(protocol.DataComponentDeathProtection) {
		return nil, false
	}
	own, ok := stack.DeathProtectionOf()
	if !ok {
		return DeathProtectionOf(stack.ItemID)
	}
	effects := make([]ConsumeEffect, 0, len(own))
	for _, e := range own {
		effects = append(effects, consumeEffectOf(e))
	}
	return effects, true
}

// StackResistsDamage reports whether the stack's damage_resistant (its own,
// else its item's: the fire and explosion tables) names damageType, so a
// blow of it cannot hurt the stack's item entity or wear it worn. False for
// a stack whose patch removes it.
// vanilla: ItemStack.canBeHurtBy, DamageResistant.isResistantTo
func StackResistsDamage(stack protocol.ItemStack, damageType DamageType) bool {
	if stack.Removes(protocol.DataComponentDamageResistant) {
		return false
	}
	if own, ok := stack.DamageResistantOf(); ok {
		if own.Tag != "" {
			return damageType.InTag(DamageTypeTagID(own.TagID))
		}
		id := int32(damageType) - 1
		return id >= 0 && slices.Contains(own.IDs, id)
	}
	if stack.ItemID < 0 || int(stack.ItemID) >= len(Items) {
		return false
	}
	if tag := itemDamageResistance[stack.ItemID]; tag != noDamageTypeTag {
		return damageType.InTag(tag)
	}
	return false
}

// StackSparedHurtWear reports whether a worn piece takes no wear from a blow
// of damageType: one with no equippable component or damage_on_hurt false,
// or one resistant to the type.
// vanilla: LivingEntity.doHurtEquipment
func StackSparedHurtWear(stack protocol.ItemStack, damageType DamageType) bool {
	if eq, ok := StackEquippable(stack); !ok || !eq.DamageOnHurt {
		return true
	}
	return StackResistsDamage(stack, damageType)
}

// withProtectionTags adds the stack's own glider, death_protection and
// damage_resistant to components as stored components.
func withProtectionTags(components nbt.Compound, stack protocol.ItemStack) nbt.Compound {
	death, hasDeath := stack.DeathProtectionOf()
	resistant, hasResistant := stack.DamageResistantOf()
	glider := stack.HasGlider()
	if (glider || hasDeath || hasResistant) && components == nil {
		components = nbt.Compound{}
	}
	if glider {
		components["minecraft:glider"] = &nbt.OrderedCompound{}
	}
	if hasDeath {
		effects := nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{}}
		for _, e := range death {
			effects.Items = append(effects.Items, consumeEffectTag(e))
		}
		tag := nbt.NewOrderedCompound(1)
		tag.Set("death_effects", effects)
		components["minecraft:death_protection"] = tag
	}
	if hasResistant {
		tag := nbt.NewOrderedCompound(1)
		tag.Set("types", damageSetTag(resistant))
		components["minecraft:damage_resistant"] = tag
	}
	return components
}

// readProtectionComponent lays the named glider, death_protection or
// damage_resistant value on the stack; false for a value the codec refuses,
// modeled false for any other name.
// vanilla: Unit.CODEC, DeathProtection.CODEC, DamageResistant.CODEC
func (lookup *TagLookup) readProtectionComponent(stack *protocol.ItemStack, name string, value nbt.Tag) (ok, modeled bool) {
	switch name {
	case "minecraft:glider":
		// Unit.CODEC takes any map and keeps nothing of it.
		if _, isMap := nbt.CompoundView(value); !isMap {
			return false, true
		}
		stack.SetGlider()
	case "minecraft:death_protection":
		entry, isMap := nbt.CompoundView(value)
		if !isMap {
			return false, true
		}
		var effects []protocol.ConsumeEffectComponent
		if field, has := entry["death_effects"]; has {
			if effects, ok = consumeEffectsOf(field); !ok {
				return false, true
			}
		}
		stack.SetDeathProtection(effects)
	case "minecraft:damage_resistant":
		entry, isMap := nbt.CompoundView(value)
		if !isMap {
			return false, true
		}
		field, has := entry["types"]
		if !has {
			return false, true
		}
		set, ok := lookup.damageSetOf(field)
		if !ok {
			return false, true
		}
		stack.SetDamageResistant(set)
	default:
		return false, false
	}
	return true, true
}
