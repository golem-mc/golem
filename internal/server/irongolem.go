package server

import (
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/registry"
)

// repairIronGolem heals an iron golem with the held ingot, passing at full health.
// vanilla: IronGolem.mobInteract, Mob.interact
func (c *client) repairIronGolem(ent *mobEntity, hand int32) (bool, error) {
	s := c.srv
	s.mobsMu.Lock()
	m := ent.mob
	if !m.HealIronGolem() {
		s.mobsMu.Unlock()
		return false, nil
	}
	pitch := 1 + (m.Rand.Float32()-m.Rand.Float32())*0.2
	x, y, z, silent := m.X, m.Y, m.Z, m.Silent
	s.mobsMu.Unlock()
	if !silent {
		s.broadcastSoundID(ent.w, registry.SoundEntityIronGolemRepair, protocol.SoundCategoryNeutral, x, y, z, 1, pitch)
	}
	c.spendHeld(hand)
	s.gameEvent(ent.w, rules.GameEventEntityInteract, x, y, z, c.gameEventContext(0))
	return true, nil
}
