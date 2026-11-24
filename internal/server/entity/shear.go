package entity

import (
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/registry"
	"github.com/google/uuid"
)

// wolfShearDropHeight is the wolf's passenger attachment height above the
// feet.
const wolfShearDropHeight = 0.8

// ShearedEquipment is a piece shears took off a mob.
type ShearedEquipment struct {
	Stack protocol.ItemStack
	// Sound is the equippable's shearing_sound.
	Sound protocol.SoundHolder
	// DropHeight is where the piece drops, above the feet.
	DropHeight float64
}

// ShearEquipment removes the first eligible stack in equipment slot order.
// vanilla: Mob.canShearEquipment, Mob.attemptToShearEquipment
func (m *Mob) ShearEquipment(clicker uuid.UUID, creative bool) (ShearedEquipment, bool) {
	if !m.Alive() {
		return ShearedEquipment{}, false
	}
	if m.Type == TypeWolf {
		if !m.IsOwnedBy(clicker) {
			return ShearedEquipment{}, false
		}
	} else if len(m.PassengerIDs()) > 0 {
		return ShearedEquipment{}, false
	}
	const saddleSlot = 7
	for slot := range saddleSlot + 1 {
		var stack protocol.ItemStack
		if slot < saddleSlot {
			stack = m.EquipmentSlot(slot)
		} else {
			stack = m.SaddleSlot()
			if stack.Empty() {
				// /item can write Horse.Saddle on a copper golem.
				stack = m.Horse.Saddle
			}
		}
		if stack.Empty() {
			continue
		}
		eq, ok := registry.StackEquippable(stack)
		if !ok || !eq.CanBeSheared {
			continue
		}
		if !creative && player.HasBindingCurse(stack) {
			continue
		}
		switch {
		case slot < saddleSlot:
			m.SetEquipment(slot, protocol.ItemStack{})
		case m.Type == TypeCopperGolem && !m.Antenna.Empty():
			m.SetMetadataAntenna(protocol.ItemStack{})
		default:
			m.SetMetadataHorseSaddle(protocol.ItemStack{})
		}
		return ShearedEquipment{Stack: stack, Sound: eq.ShearingSound, DropHeight: m.shearDropHeight()}, true
	}
	return ShearedEquipment{}, false
}

// shearDropHeight approximates vanilla's PASSENGER attachment average.
// vanilla: Mob.shearItem
func (m *Mob) shearDropHeight() float64 {
	switch m.Type {
	case TypeHappyGhast:
		return HarnessDropHeight
	case TypeWolf:
		return wolfShearDropHeight
	}
	_, height := KindDims(m.Type)
	return height
}
