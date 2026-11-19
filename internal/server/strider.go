package server

import (
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/registry"
)

// useOnStrider handles a pig or strider click no lead or name tag took:
// food feeds it, a saddled one mounts, a held saddle is fitted.
// vanilla: Pig.mobInteract, Strider.mobInteract, Equippable.equipOnTarget
func (c *client) useOnStrider(ent *mobEntity, hand int32, held protocol.ItemStack, sneaking bool) (bool, error) {
	if ent.mob.IsStrider() && entity.IsStriderFood(held) || ent.mob.Type == entity.TypePig && ent.mob.FedBy(held) {
		return c.feedMob(ent, hand)
	}
	s := c.srv
	s.mobsMu.Lock()
	m := ent.mob
	click := m.StriderUseItem(held, sneaking)
	x, y, z := m.X, m.Y, m.Z
	sound := m.EquipSoundOverride("saddle")
	s.mobsMu.Unlock()
	switch click {
	case entity.StriderClickSaddle:
		s.broadcastSoundID(ent.w, sound, protocol.SoundCategoryNeutral, x, y, z, 1, 1)
		return true, c.consumeHeld(hand)
	case entity.StriderClickMount:
		s.seatPlayerOnMob(c, ent, false)
		return true, nil
	}
	return false, nil
}

// holdsSteeringStick reports whether either hand holds this kind's stick. vanilla: LivingEntity.isHolding
func (c *client) holdsSteeringStick(kind entity.MobType) bool {
	return entity.IsSteeringStick(kind, c.heldItem(protocol.HandMain)) ||
		entity.IsSteeringStick(kind, c.heldItem(protocol.HandOff))
}

// brokenStick is the fishing rod a worn-out stick becomes, keeping its
// other components. vanilla: ItemStack.hurtAndConvertOnBreak
func brokenStick(stick protocol.ItemStack) protocol.ItemStack {
	stick.ItemID, stick.Count, stick.Damage = int32(registry.ItemFishingRod), 1, 0
	return stick
}

// handleUseCarrotStick uses the pig's steering stick.
func (c *client) handleUseCarrotStick(hand int32) bool {
	return c.useSteeringStick(hand, entity.TypePig, 7)
}

// handleUseSteeringStick boosts the steered strider, wearing the stick by
// one; a broken stick becomes a fishing rod. Only a use that boosts nothing
// counts as used. vanilla: FoodOnAStickItem.use
func (c *client) handleUseSteeringStick(hand int32) bool {
	return c.useSteeringStick(hand, entity.TypeStrider, 1)
}

func (c *client) useSteeringStick(hand int32, kind entity.MobType, wear int32) bool {
	if c.player.Vitals().Dead || c.player.GameMode() == protocol.GameModeSpectator ||
		!entity.IsSteeringStick(kind, c.heldItem(hand)) {
		return false
	}
	ent := c.mount.Load()
	if ent == nil {
		c.awardUsedHeld(hand)
		return false
	}
	stick := c.holdsSteeringStick(ent.mob.Type)
	s := c.srv
	s.mobsMu.Lock()
	m := ent.mob
	// Holding the stick makes the rider the controller.
	// vanilla: Strider.getControllingPassenger
	m.Horse.StickHeld = stick
	boosted := ent.rider == c && m.Type == kind && m.Horse.Controlled() && m.ItemSteeringBoost()
	s.mobsMu.Unlock()
	if !boosted {
		c.awardUsedHeld(hand)
		return false
	}
	c.useSwing = swingServer
	if c.player.GameMode() == protocol.GameModeCreative {
		return true
	}
	if wear = c.pluginItemDamageHeld(hand, wear); wear <= 0 {
		return true
	}
	var w slotWear
	c.player.Inventory(func(inv *player.Inventory) {
		slot := rules.SlotOfHand(inv, hand)
		before := inv.Slots[slot]
		w = c.wearUnder(inv, slot, rules.ProcessDurabilityChange(before, wear, c.ownDice()))
		if w.broke {
			w.payload, w.visible = c.setSlotUnder(inv, slot, brokenStick(before))
		}
	})
	c.tellWear(w)
	return true
}
