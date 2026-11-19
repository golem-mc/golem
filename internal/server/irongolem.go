package server

import (
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/registry"
)

// repairIronGolem heals an iron golem 25 with the held ingot; at full health
// the click passes and nothing is spent.
// vanilla: IronGolem.mobInteract
func (c *client) repairIronGolem(ent *mobEntity, hand int32) (bool, error) {
	s := c.srv
	s.mobsMu.Lock()
	m := ent.mob
	healed := m.HealIronGolem()
	pitch := float32(1 + (m.Rand.Float64()-m.Rand.Float64())*0.2)
	x, y, z := m.X, m.Y, m.Z
	s.mobsMu.Unlock()
	if !healed {
		return false, nil
	}
	s.broadcastSoundID(ent.w, registry.SoundEntityIronGolemRepair, protocol.SoundCategoryNeutral, x, y, z, 1, pitch)
	c.spendHeld(hand)
	return true, nil
}
