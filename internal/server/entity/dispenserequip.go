package entity

import (
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/registry"
)

// DispenserEquip puts one piece a dispenser fires at the mob on, in the slot
// its equippable component names, and reports whether it fit. The item must
// allow the mob's kind and the slot must be bare. Most mobs need loot pickup;
// tamed equines and nautiluses, grown happy ghasts' harnesses, and pigs' and
// striders' saddles bypass it.
// vanilla: LivingEntity.canEquipWithDispenser, EquipmentDispenseItemBehavior.dispenseEquipment
func (m *Mob) DispenserEquip(stack protocol.ItemStack) bool { return m.dispenserEquip(stack, true) }

// CanDispenserEquip reports whether DispenserEquip would fit the piece, changing nothing.
func (m *Mob) CanDispenserEquip(stack protocol.ItemStack) bool { return m.dispenserEquip(stack, false) }

func (m *Mob) dispenserEquip(stack protocol.ItemStack, commit bool) bool {
	if !m.Alive() {
		return false
	}
	item, ok := registry.StackEquippable(stack)
	if !ok || !item.AllowsEntity(m.Type.Name()) {
		return false
	}
	// vanilla: Dolphin.canDispenserEquipIntoSlot accepts only the main hand.
	if m.Type == TypeDolphin && item.Slot != "mainhand" {
		return false
	}
	one := stack
	one.Count = 1
	switch item.Slot {
	case "body":
		// vanilla: AbstractHorse.canDispenserEquipIntoSlot, Mob.canDispenserEquipIntoSlot
		tamedHorse := IsEquine(m.Type) && m.Horse.Tame
		if !m.CanUseSlot("body") || !m.Body.Empty() || (!m.NautilusCanEquip() && !m.canTakeHarness() && !tamedHorse && !m.CanPickUpLoot) {
			return false
		}
		if commit {
			m.SetEquipment(6, one)
		}
	case "saddle":
		// vanilla: Strider.canDispenserEquipIntoSlot and
		// AbstractHorse.canDispenserEquipIntoSlot skip the loot check
		if (!m.NautilusCanEquip() || !m.Horse.Saddle.Empty()) && !m.StriderCanTakeSaddle() && !m.CanTakeSaddle() {
			return false
		}
		if commit {
			m.SetMetadataHorseSaddle(one)
		}
	default:
		// vanilla: Fox.canDispenserEquipIntoSlot takes the main hand alone
		slot := lootSlotIndex(item.Slot)
		if slot < 0 || !m.CanPickUpLoot || m.Type == TypeFox || !m.slotStack(slot).Empty() {
			return false
		}
		if commit {
			m.setSlotStack(slot, one)
			m.DropChances[slot] = guaranteedDropChance
		}
	}
	// vanilla: Mob.setPersistenceRequired
	if commit {
		m.Persistence = true
	}
	return true
}

// EquipSoundOverride returns the mob's replacement sound, or SoundNone for the item's own.
// vanilla: Strider.getEquipSound
func (m *Mob) EquipSoundOverride(slot string) registry.Sound {
	if slot == "saddle" && IsNautilus(m.Type) {
		if m.waterFluid.eyesIn {
			return registry.SoundItemNautilusSaddleUnderwaterEquip
		}
		return registry.SoundItemNautilusSaddleEquip
	}
	if slot == "saddle" && m.Type == TypePig {
		return registry.SoundEntityPigSaddle
	}
	if slot == "saddle" && m.IsStrider() {
		return registry.SoundEntityStriderSaddle
	}
	return registry.SoundNone
}

// EquipOnInteract fits one of a clicked stack whose equippable component has
// equip_on_interact into the empty slot it names, if the mob may wear it
// there, marking the slot a guaranteed drop, and reports whether it did.
// vanilla: Equippable.equipOnTarget, LivingEntity.isEquippableInSlot
func (m *Mob) EquipOnInteract(stack protocol.ItemStack) (registry.EquippableItem, bool) {
	item, ok := registry.StackEquippable(stack)
	if !ok || !item.EquipOnInteract || !m.Alive() {
		return item, false
	}
	fits := m.CanUseSlot(item.Slot) && item.AllowsEntity(m.Type.Name())
	if m.Type == TypeSulfurCube && item.Slot == "body" {
		// This override bypasses canUseSlot and allowed_entities, including for babies.
		// vanilla: SulfurCube.isEquippableInSlot
		fits = registry.Item(stack.ItemID).InTag(registry.ItemTagIDSulfurCubeSwallowable)
	}
	if !fits {
		return item, false
	}
	one := stack
	one.Count = 1
	if item.Slot == "body" {
		if !m.Body.Empty() {
			return item, false
		}
		m.SetEquipment(6, one)
		return item, true
	}
	if item.Slot == "saddle" {
		if !m.SaddleSlot().Empty() {
			return item, false
		}
		if m.Type == TypeCopperGolem {
			m.SetMetadataAntenna(one)
		} else {
			m.SetMetadataHorseSaddle(one)
		}
		return item, true
	}
	i := lootSlotIndex(item.Slot)
	if i < 0 || !m.slotStack(i).Empty() {
		return item, false
	}
	m.setSlotStack(i, one)
	m.DropChances[i] = guaranteedDropChance
	return item, true
}
