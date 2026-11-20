package server

import (
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/registry"
)

// useOnCat handles a click on a cat: a wild one eats cat food and may be
// tamed; the owner heals a hurt one, breeds a healthy one or toggles
// sitting; others' food can only start love. The owner's dye recolours the
// collar, and one the collar already wears sits the cat like any click.
// vanilla: Cat.mobInteract
func (c *client) useOnCat(ent *mobEntity, hand int32, held protocol.ItemStack) (bool, error) {
	s := c.srv
	s.mobsMu.Lock()
	m := ent.mob
	if !m.Alive() {
		s.mobsMu.Unlock()
		return true, nil
	}
	food := m.TemptedBy(held)
	switch {
	case !m.Tamable.Tame:
		if !food {
			s.mobsMu.Unlock()
			return false, nil
		}
		tamed := m.RollTame()
		if tamed {
			s.mobsMu.Unlock()
			tamed = c.tameAllowed(ent)
			s.mobsMu.Lock()
			if tamed {
				m.TameTo(c.player.UUID)
			}
		}
		m.Persistence = true
		eat, silent, x, y, z, variant := m.Sounds().Eat, m.Silent, m.X, m.Y, m.Z, m.Variant
		s.mobsMu.Unlock()
		c.spendHeld(hand)
		if !silent {
			s.broadcastSoundID(ent.w, eat, mobSoundCategory(m.Type), x, y, z, 1.0, 1.0)
		}
		status := uint8(protocol.EntityEventTameFailed)
		if tamed {
			status = protocol.EntityEventTamed
			c.tameAnimalAdvancements(m.Type.Name(), variant, s.mobLoot(ent))
		}
		s.broadcastWorld(ent.w, protocol.IDPlayEntityEvent, entityEventPayload(m.ID, status))
		return true, nil
	case !m.IsOwnedBy(c.player.UUID):
		s.mobsMu.Unlock()
		if food {
			return c.feedMob(ent, hand)
		}
		return false, nil
	case registry.Item(held.ItemID).InTag(registry.ItemTagIDCatCollarDyes):
		colour, _ := rules.DyeItemColour(held)
		if m.DyeCollar(colour) {
			m.Persistence = true
			s.mobsMu.Unlock()
			c.spendHeld(hand)
			return true, nil
		}
	}
	if food {
		if m.FeedTamed(held) {
			eat, silent, x, y, z := m.Sounds().Eat, m.Silent, m.X, m.Y, m.Z
			s.mobsMu.Unlock()
			c.spendHeld(hand)
			if !silent {
				s.broadcastSoundID(ent.w, eat, mobSoundCategory(m.Type), x, y, z, 1.0, 1.0)
			}
			return true, nil
		}
		s.mobsMu.Unlock()
		if acted, err := c.feedMob(ent, hand); acted || err != nil {
			return true, err
		}
		s.mobsMu.Lock()
	}
	c.useSwing = swingPredicted
	m.ToggleSit()
	s.mobsMu.Unlock()
	return true, nil
}

// useOnOcelot handles a click on an ocelot: fish from a player within three
// blocks while luring rolls trust; other fish starts love.
// vanilla: Ocelot.mobInteract
func (c *client) useOnOcelot(ent *mobEntity, hand int32, held protocol.ItemStack) (bool, error) {
	s := c.srv
	s.mobsMu.Lock()
	m := ent.mob
	pos := c.player.Position()
	near := entity.Dist2(pos.X, pos.Y, pos.Z, m.X, m.Y, m.Z) < 9
	food := m.TemptedBy(held)
	if !m.Alive() || !food || m.Trusting || !m.TemptRunning() || !near {
		s.mobsMu.Unlock()
		if food {
			return c.feedMob(ent, hand)
		}
		return false, nil
	}
	trusted := m.TryToTrust()
	s.mobsMu.Unlock()
	c.spendHeld(hand)
	status := uint8(protocol.EntityEventTrustFailed)
	if trusted {
		status = protocol.EntityEventTrusted
	}
	s.broadcastWorld(ent.w, protocol.IDPlayEntityEvent, entityEventPayload(m.ID, status))
	return true, nil
}
