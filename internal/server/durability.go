package server

import (
	mrand "math/rand"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/registry"
)

// Item wear's client side: which slot wore, the slot correction, the break
// broadcast. The wear itself is in package rules.

// ownDice stands in for vanilla's LivingEntity random per connection
// (unbreaking rolls, throw spread), created lazily.
func (c *client) ownDice() *mrand.Rand {
	c.ownMu.Lock()
	defer c.ownMu.Unlock()
	if c.ownRng == nil {
		c.ownRng = newDice(c.srv.seedFor("player", int64(c.player.EntityID)))
	}
	return c.ownRng
}

// setSlotUnder writes stack into slot under the caller's inventory lock,
// returning the slot packet and visibility for tellWear to send outside it.
func (c *client) setSlotUnder(inv *player.Inventory, slot int, stack protocol.ItemStack) (payload []byte, visible bool) {
	inv.Set(slot, stack)
	return inv.WindowSlot(playerWindowID, int32(slot), stack), player.VisibleSlot(inv, slot)
}

// slotWear is wear put on one slot under the inventory lock, for tellWear
// to send outside it: the slot packet (nil if unchanged), whether the slot
// shows on the body, whether the item broke, and which item wore, 0 for a
// repair, which fires no trigger.
type slotWear struct {
	payload        []byte
	slot           int
	visible, broke bool
	item           int32
	// before is the stack as it was, and durability and delta what
	// item_durability_changed tests.
	// vanilla: ItemDurabilityTrigger.TriggerInstance.matches
	before            protocol.ItemStack
	durability, delta int32
}

// wearUnder puts n wear on the slot's stack under the caller's inventory
// lock.
func (c *client) wearUnder(inv *player.Inventory, slot int, n int32) slotWear {
	was := inv.Slots[slot]
	worn, breaks := rules.Wear(was, n)
	if worn.Damage == was.Damage && worn.Count == was.Count {
		return slotWear{slot: slot}
	}
	w := slotWear{slot: slot, broke: breaks, item: was.ItemID,
		before: was, durability: registry.StackMaxDamage(was) - (was.Damage + n), delta: -n}
	w.payload, w.visible = c.setSlotUnder(inv, slot, worn)
	if breaks {
		// vanilla: ServerPlayer.onEquippedItemBroken
		c.awardItemStat(registry.StatTypeBroken, was.ItemID, 1)
	}
	return w
}

// tellWear sends the slot correction, the break event to all, and the
// equipment update, and fires item_durability_changed. Call it outside
// every lock; it goes via the mailbox.
func (c *client) tellWear(w slotWear) {
	if w.payload == nil {
		return
	}
	if w.item != 0 {
		c.woreItemAdvancements(w.before, w.durability, w.delta)
	}
	if w.broke {
		if status, ok := rules.BreakStatus(w.slot); ok {
			c.srv.broadcastWorld(c.theWorld(), protocol.IDPlayEntityEvent,
				entityEventPayload(c.player.EntityID, status))
		}
		c.log.Debug("item broke", "slot", w.slot)
		c.pluginItemBreak(w.item)
	}
	c.send(protocol.IDPlayContainerSlot, w.payload)
	if w.visible {
		c.broadcastEquipment()
	}
}

// wearHeld puts n wear (after unbreaking) on the held item, none in
// creative. Armour wears through armourAbsorb instead.
// vanilla: ItemStack.hurtAndBreak, processDurabilityChange.
func (c *client) wearHeld(hand int32, n int32) {
	c.wearHeldWith(hand, n, rules.ProcessDurabilityChange)
}

// wearHeldWith applies the item's durability policy before updating its slot.
func (c *client) wearHeldWith(hand int32, n int32, process func(protocol.ItemStack, int32, *mrand.Rand) int32) {
	if n <= 0 || c.player.GameMode() == protocol.GameModeCreative {
		return
	}
	if n = c.pluginItemDamageHeld(hand, n); n <= 0 {
		return
	}
	var w slotWear
	c.player.Inventory(func(inv *player.Inventory) {
		slot := rules.SlotOfHand(inv, hand)
		w = c.wearUnder(inv, slot, process(inv.Slots[slot], n, c.ownDice()))
	})
	c.tellWear(w)
}

// wearAttackWeapon puts n wear on the stack the blow used, the spin's
// starting slot while it is in flight. Armour wears through armourAbsorb.
// vanilla: Player.itemAttackInteraction, ItemStack.hurtAndBreak
func (c *client) wearAttackWeapon(n int32) {
	if n <= 0 || c.player.GameMode() == protocol.GameModeCreative {
		return
	}
	if !c.player.Appearance().SpinAttack {
		c.wearHeld(protocol.HandMain, n)
		return
	}
	c.spinMu.Lock()
	weapon := c.spin.weapon
	c.spinMu.Unlock()
	identity := weapon.StackIdentity()
	if n = c.pluginItemDamage(weapon, n); n <= 0 {
		return
	}

	var w slotWear
	c.player.Inventory(func(inv *player.Inventory) {
		slot := -1
		for index, stack := range inv.Slots {
			if stack.StackIdentity() == identity {
				slot = index
				break
			}
		}
		if slot < 0 {
			return
		}
		w = c.wearUnder(inv, slot, rules.ProcessDurabilityChange(inv.Slots[slot], n, c.ownDice()))
	})
	c.tellWear(w)
}

// hurtHelmet wears the helmet for a damages_helmet blow, a quarter of the
// blow (at least one) after unbreaking, reporting whether one was worn.
// vanilla: LivingEntity.hurtHelmet
func (c *client) hurtHelmet(damage float64) bool {
	var w slotWear
	var worn bool
	n := rules.ArmourWearPerPiece(damage)
	if asked := c.askWear(n, nil, player.ArmorSlot); asked != nil {
		n = asked[0]
	}
	c.player.Inventory(func(inv *player.Inventory) {
		stack := inv.Slots[player.ArmorSlot]
		if stack.Empty() {
			return
		}
		worn = true
		w = c.wearUnder(inv, player.ArmorSlot, rules.ProcessDurabilityChange(stack, n, c.ownDice()))
	})
	c.tellWear(w)
	return worn
}
