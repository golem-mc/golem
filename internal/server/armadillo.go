package server

import (
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/registry"
)

// brushArmadillo applies Armadillo.brushOffScute to an adult; only a
// player's brush fires player_interacted_with_entity, a dispenser's does not.
// vanilla: ServerGamePacketListenerImpl.handleInteract
func (c *client) brushArmadillo(ent *mobEntity, hand int32, held protocol.ItemStack) error {
	if !c.srv.brushArmadillo(ent, held, c.LootEntity()) {
		return nil
	}
	c.srv.mobsMu.Lock()
	after := c.srv.bodyInteraction(ent)
	c.srv.mobsMu.Unlock()
	c.wearHeld(hand, 16)
	c.interactedWithEntityAdvancements(after, held.ItemID)
	return nil
}

// brushArmadillo shares the brush loot, sound and game event between hands and dispensers.
func (s *Server) brushArmadillo(ent *mobEntity, held protocol.ItemStack, interacting rules.LootEntity) bool {
	s.mobsMu.Lock()
	m := ent.mob
	if !m.Alive() || m.IsBaby() {
		s.mobsMu.Unlock()
		return false
	}
	var name *protocol.Text
	if text, _, ok := mobCustomName(ent); ok {
		name = &text
	}
	drops := mobDrops{w: ent.w, stacks: m.RollArmadilloBrush(held, name, interacting, s.lootLevelAt(ent.w, m.X, m.Y, m.Z)), x: m.X, y: m.Y, z: m.Z}
	x, y, z, silent := m.X, m.Y, m.Z, m.Silent
	s.mobsMu.Unlock()
	s.scatterDrops(drops)
	if !silent {
		s.broadcastEntitySoundID(ent.w, nil, m.ID, registry.SoundEntityArmadilloBrush, mobSoundCategory(m.Type), x, y, z, 1, 1)
	}
	s.gameEvent(ent.w, rules.GameEventEntityInteract, x, y, z, rules.GameEventContext{Mob: ent.uuid})
	return true
}

func (t *mobDimTick) shedScute(fx *mobEffects, m *entity.Mob) {
	stacks := m.RollArmadilloShed(t.s.lootLevelAt(t.w, m.X, m.Y, m.Z))
	if len(stacks) == 0 {
		return
	}
	w := t.w
	fx.deaths = append(fx.deaths, mobDrops{w: w, stacks: stacks, x: m.X, y: m.Y, z: m.Z})
	fx.voiceID(w, m, registry.SoundEntityArmadilloScuteDrop, 1, (m.Rand.Float32()-m.Rand.Float32())*0.2+1)
	fx.gameEvent(w, rules.GameEventEntityPlace, m.X, m.Y, m.Z, rules.GameEventContext{Mob: t.h.book[m].uuid})
}
