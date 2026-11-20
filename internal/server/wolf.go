package server

import (
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/advancement"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/registry"
)

// useOnWolf handles a click on a wolf. A wild one may be tamed with a
// bone. A tame one, in order: food heals it when hurt, the owner's dye
// recolours the collar, anyone's food goes to breeding and growing, and the
// owner's other clicks toggle sitting. The owner can armor a grown wolf,
// mend a sitting one's armor with a scute, and shear the armor off.
// vanilla: Wolf.mobInteract, Entity.interact
func (c *client) useOnWolf(ent *mobEntity, hand int32, held protocol.ItemStack, sneaking bool) (bool, error) {
	s := c.srv
	s.mobsMu.Lock()
	m := ent.mob
	if !m.Alive() {
		s.mobsMu.Unlock()
		return true, nil
	}
	item := registry.Item(held.ItemID)
	if m.Tamable.Tame {
		owner := m.IsOwnedBy(c.player.UUID)
		if registry.Item(held.ItemID).InTag(registry.ItemTagIDWolfCollarDyes) && owner {
			// A dye the collar already wears is a pass: no toggle either.
			colour, _ := rules.DyeItemColour(held)
			changed := m.DyeCollar(colour)
			s.mobsMu.Unlock()
			if !changed {
				return false, nil
			}
			c.spendHeld(hand)
			return true, nil
		}
		healed := m.FeedWolf(held)
		equipped := !healed && m.EquipWolfArmor(held, owner)
		repaired := !healed && !equipped && m.RepairWolfArmor(held.ItemID, owner)
		x, y, z := m.X, m.Y, m.Z
		after := s.bodyInteraction(ent)
		s.mobsMu.Unlock()
		switch {
		case healed:
			c.spendHeld(hand)
			return true, nil
		case equipped:
			c.spendHeld(hand)
			s.broadcastSoundID(ent.w, registry.SoundItemArmorEquipWolf, protocol.SoundCategoryNeutral, x, y, z, 1, 1)
			return true, nil
		case repaired:
			c.spendHeld(hand)
			s.broadcastSoundID(ent.w, registry.SoundItemWolfArmorRepair, protocol.SoundCategoryNeutral, x, y, z, 1, 1)
			c.interactedWithEntityAdvancements(after, held.ItemID)
			return true, nil
		}
		if entity.IsWolfFood(held.ItemID) {
			if fed, err := c.feedMob(ent, hand); fed || err != nil {
				return true, err
			}
		}
		if !owner {
			return false, nil
		}
		s.mobsMu.Lock()
		c.useSwing = swingPredicted
		m.ToggleSit()
		s.mobsMu.Unlock()
		return true, nil
	}
	if item != registry.ItemBone || m.Angry() {
		s.mobsMu.Unlock()
		if entity.IsWolfFood(held.ItemID) {
			return c.feedMob(ent, hand)
		}
		return false, nil
	}
	// Make the attribute map before taming raises the maximum, so the rise is
	// sent as a change.
	if m.Attributes() == nil {
		m.SetAttributes(m.BuildAttributes(savedAttributes(ent.saved)))
	}
	tamed := m.RollTame()
	if tamed {
		// The listener runs with no lock held.
		s.mobsMu.Unlock()
		tamed = c.tameAllowed(ent)
		s.mobsMu.Lock()
		if tamed {
			m.TameTo(c.player.UUID)
		}
	}
	variant := m.Variant
	if tamed {
		m.Attributes().SetBase(registry.AttributeMaxHealth, m.MaxHealth)
	}
	s.mobsMu.Unlock()
	c.spendHeld(hand)
	status := uint8(protocol.EntityEventTameFailed)
	if tamed {
		status = protocol.EntityEventTamed
		c.tameAnimalAdvancements(m.Type.Name(), variant, s.mobLoot(ent))
	}
	s.broadcastWorld(ent.w, protocol.IDPlayEntityEvent, entityEventPayload(m.ID, status))
	c.useSwing = swingServer
	return true, nil
}

// bodyInteraction is a mob, its variant and its body slot as an advancement
// criterion reads it.
func (s *Server) bodyInteraction(ent *mobEntity) advancement.EntityInteraction {
	m := ent.mob
	e := advancement.EntityInteraction{Type: m.Type.Name(), Baby: m.IsBaby(), Variant: m.Variant, Loot: s.mobLoot(ent)}
	if !m.Body.Empty() {
		e.Body, e.BodyDamage = m.Body.ItemID, m.Body.Damage
	}
	return e
}

// wolfScuteParticles is the armadillo scute spray of cracking armor.
// vanilla: Wolf.actuallyHurt
func wolfScuteParticles(m *entity.Mob) []byte {
	const id = protocol.ParticleItem
	var data protocol.Buffer
	data.PutSlot(protocol.ItemStack{ItemID: int32(registry.ItemArmadilloScute), Count: 1})
	return particleDataPayload(id, data.Bytes(), false, m.X, m.Y+1, m.Z, 0.2, 0.1, 0.2, 0.1, 20)
}
