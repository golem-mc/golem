package server

import (
	"math"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/effect"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/registry"
)

// parrotPoisonTicks is the cookie's Poison duration.
const parrotPoisonTicks = 900

// useOnParrot handles a click on a parrot: its seeds tame a wild one, a
// cookie kills it, and the owner's click toggles sitting while it stands on
// the ground.
// vanilla: Parrot.mobInteract
func (c *client) useOnParrot(ent *mobEntity, hand int32, held protocol.ItemStack) (bool, error) {
	s := c.srv
	item := registry.Item(held.ItemID)
	s.mobsMu.Lock()
	m := ent.mob
	if !m.Alive() {
		s.mobsMu.Unlock()
		return true, nil
	}
	switch {
	case !m.Tamable.Tame && item.InTag(registry.ItemTagIDParrotFood):
		pitch := 1 + (m.Rand.Float32()-m.Rand.Float32())*0.2
		silent := m.Silent
		x, y, z := m.X, m.Y, m.Z
		tamed := m.TameParrot(c.player.UUID)
		s.mobsMu.Unlock()
		c.spendHeld(hand)
		if !silent {
			s.broadcastSoundID(ent.w, registry.SoundEntityParrotEat, protocol.SoundCategoryNeutral, x, y, z, 1, pitch)
		}
		status := uint8(protocol.EntityEventTameFailed)
		if tamed {
			status = protocol.EntityEventTamed
			c.tameAnimalAdvancements(m.Type.Name(), "", s.mobLoot(ent))
		}
		s.broadcastWorld(ent.w, protocol.IDPlayEntityEvent, entityEventPayload(m.ID, status))
	case item.InTag(registry.ItemTagIDParrotPoisonousFood):
		creative := c.player.GameMode() == protocol.GameModeCreative
		m.AddEffect(effect.Instance{ID: registry.MobEffectPoison, Duration: parrotPoisonTicks})
		var hurt *mobHurt
		var softened bool
		if creative || !m.Invulnerable {
			pos := c.player.Position()
			attacker := entity.Player{X: pos.X, Y: pos.Y, Z: pos.Z, Yaw: float64(pos.Yaw), Pitch: float64(pos.Pitch),
				EntityID: c.player.EntityID, Huntable: true, Creative: creative}
			hurt, _, _, softened = landMobHurt(ent, math.MaxFloat32, attacker, 0, protocol.ItemStack{}, rules.EnchContext{}, nil)
		}
		s.mobsMu.Unlock()
		c.spendHeld(hand)
		if hurt != nil && !softened {
			s.broadcastMobHurt(ent.w, hurt)
		}
	case m.Tamable.Tame && m.IsOwnedBy(c.player.UUID) && m.OnGround:
		m.ToggleSit()
		s.mobsMu.Unlock()
	default:
		s.mobsMu.Unlock()
		return false, nil
	}
	return true, nil
}
