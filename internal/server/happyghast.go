package server

import (
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/registry"
)

// useOnHappyGhast handles a click on a happy ghast no lead or name tag took:
// shears take a harness off, a grown bare body takes a held harness, a
// harnessed one seats the player on a plain click, and food goes to feeding.
// vanilla: HappyGhast.mobInteract, Entity.interact, Equippable.equipOnTarget
func (c *client) useOnHappyGhast(ent *mobEntity, hand int32, held protocol.ItemStack, sneaking bool) (bool, error) {
	s := c.srv
	s.mobsMu.Lock()
	m := ent.mob
	baby := m.IsBaby()
	equipped := !baby && m.EquipHarness(held)
	harnessed := m.WearsHarness()
	x, y, z, silent := m.X, m.Y, m.Z, m.Silent
	s.mobsMu.Unlock()
	if !baby && !equipped && c.equipOnInteract(ent, hand, held) {
		return true, nil
	}
	switch {
	case equipped:
		c.spendHeld(hand)
		if !silent {
			// vanilla: LivingEntity.onEquipItem plays the item's equip sound
			s.broadcastSoundID(ent.w, registry.SoundEntityHappyGhastEquip, protocol.SoundCategoryNeutral, x, y, z, 1, 1)
		}
	case harnessed && !sneaking:
		// A full ghast still takes the click.
		// vanilla: HappyGhast.doPlayerRide
		s.seatPlayerOnMob(c, ent, false)
	case m.FedBy(held):
		return c.feedMob(ent, hand)
	default:
		return false, nil
	}
	return true, nil
}
