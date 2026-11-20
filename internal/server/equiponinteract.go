package server

import (
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/rules"
)

// equipOnInteract is the held item's turn after the mob's own click passed:
// a stack with equip_on_interact goes on the mob, one of it spent outside
// creative; the tick sends the equipment.
// vanilla: Player.interactOn, ItemStack.interactLivingEntity,
// Equippable.equipOnTarget, LivingEntity.onEquipItem
func (c *client) equipOnInteract(ent *mobEntity, hand int32, held protocol.ItemStack) bool {
	s := c.srv
	s.mobsMu.Lock()
	m := ent.mob
	item, equipped := m.EquipOnInteract(held)
	fx := equipFx{x: m.X, y: m.Y, z: m.Z, category: mobSoundCategory(m.Type), silent: m.Silent, event: true,
		sound: m.EquipSoundOverride(item.Slot), ctx: rules.GameEventContext{Mob: ent.uuid}}
	after := s.bodyInteraction(ent)
	s.mobsMu.Unlock()
	if !equipped {
		return false
	}
	c.useSwing = swingPredicted
	s.equipFeedback(ent.w, item, fx)
	s.gameEvent(ent.w, rules.GameEventEntityInteract, fx.x, fx.y, fx.z, c.gameEventContext(0))
	c.spendHeld(hand)
	c.interactedWithEntityAdvancements(after, held.ItemID)
	return true
}
