package server

import (
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/effect"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/registry"
)

// useOnNautilus follows AbstractNautilus.mobInteract after leads and name tags.
func (c *client) useOnNautilus(ent *mobEntity, hand int32, held protocol.ItemStack, sneaking bool) (bool, error) {
	s := c.srv
	s.mobsMu.Lock()
	m := ent.mob
	baby, tame, food := m.IsBaby(), m.Tamable.Tame, m.NautilusFood(held)
	if !baby && tame && sneaking {
		s.mobsMu.Unlock()
		return true, c.openMountScreen(ent)
	}
	ate, tamed, equipped := false, false, false
	status := uint8(protocol.EntityEventHorseTameFailed)
	if food && !baby && !tame {
		ate = true
		tamed = m.TameNautilus(c.player.UUID)
		if tamed {
			status = protocol.EntityEventHorseTamed
		}
	} else if food {
		ate = m.FeedNautilus(held)
	}
	sound := m.NautilusSound(entity.NautilusEat).Holder()
	if !food && !baby {
		switch {
		case m.Horse.Saddle.Empty() && m.Wears("saddle", held):
			held.Count = 1
			m.SetEquipment(protocol.EquipSaddle, held)
			m.SaddleDropChance = entity.GuaranteedDropChance
			equipped = true
			sound = m.EquipSoundOverride("saddle").Holder()
		case m.Body.Empty() && m.Wears("body", held):
			held.Count = 1
			m.SetEquipment(6, held)
			m.BodyDropChance = entity.GuaranteedDropChance
			equipped = true
			item, _ := registry.StackEquippable(held)
			sound = item.EquipSound
		}
	}
	volume, pitch := float32(1), float32(1)
	if ate {
		volume, pitch = m.SoundVolume(), m.VoicePitch()
	}
	x, y, z, silent, id := m.X, m.Y, m.Z, m.Silent, m.ID
	s.mobsMu.Unlock()
	if ate || equipped {
		if ate && !baby && !tame {
			c.useSwing = swingServer
			s.broadcastWorld(ent.w, protocol.IDPlayEntityEvent, entityEventPayload(id, status))
		}
		if tamed {
			c.tameAnimalAdvancements(m.Type.Name(), "", s.mobLoot(ent))
		}
		if !silent {
			s.broadcastSoundHolder(ent.w, sound, protocol.SoundCategoryNeutral, x, y, z, volume, pitch)
		}
		if equipped {
			return true, c.consumeHeldOutsideCreative(hand)
		}
		return true, c.consumeNautilusFood(hand, held)
	}
	if !baby && c.equipOnInteract(ent, hand, held) {
		return true, nil
	}
	if food {
		return c.feedMob(ent, hand)
	}
	if !baby && tame && !sneaking {
		s.seatPlayerOnMob(c, ent, false)
		return true, nil
	}
	return false, nil
}

func (c *client) consumeNautilusFood(hand int32, held protocol.ItemStack) error {
	// vanilla: AbstractNautilus.usePlayerItem
	if registry.Item(held.ItemID).InTag(registry.ItemTagIDNautilusBucketFood) {
		c.filledResult(hand, protocol.ItemStack{ItemID: int32(registry.ItemWaterBucket), Count: 1}, true)
		return nil
	}
	return c.consumeHeldOutsideCreative(hand)
}

func (c *client) nautilusRiderEffect() {
	// vanilla: AbstractNautilus.applyEffects; called only after releasing mobsMu.
	now, _ := c.theWorld().Time()
	const id = registry.MobEffectBreathOfTheNautilus
	for _, in := range c.player.Effects() {
		if in.ID == id && now%40 != 0 {
			return
		}
	}
	c.AddEffect(effect.New(id, 60, 0, true, true, true))
}
