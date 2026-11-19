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

// ShearEquipment takes the first shearable piece off the mob, the body slot
// before the saddle, and reports it. clicker is the player holding the
// shears; a piece with binding_curse stays on unless creative.
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
	for i, slot := range []protocol.ItemStack{m.Body, m.Horse.Saddle} {
		if slot.Empty() {
			continue
		}
		eq, ok := registry.StackEquippable(slot)
		if !ok || !eq.CanBeSheared {
			continue
		}
		if !creative && player.HasBindingCurse(slot) {
			continue
		}
		taken := ShearedEquipment{Stack: slot, Sound: eq.ShearingSound, DropHeight: m.shearDropHeight()}
		if i == 0 {
			m.SetEquipment(6, protocol.ItemStack{})
		} else {
			m.SetMetadataHorseSaddle(protocol.ItemStack{})
		}
		return taken, true
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
