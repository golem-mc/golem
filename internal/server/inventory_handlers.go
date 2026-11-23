package server

import (
	"errors"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/registry"
)

// dropSpamThreshold is vanilla's creative drop throttle, TickThrottler(20,
// 1480): 74 drops at a step of 20 fill it.
// vanilla: ServerGamePacketListenerImpl.dropSpamThrottler
const dropSpamThreshold = 1480

// handleCreativeSlot records a creative player's slot change. The client
// creates items in creative, so the stack arrives in the untrusted slot
// shape (each component value length-prefixed) and is validated. Slot -1
// is a stack dropped out of the window.
func (c *client) handleCreativeSlot(r *protocol.Reader) error {
	slot := int32(r.ReadI16())
	stack, ok := r.ReadUntrustedSlot()
	if err := r.Err(); err != nil {
		// Deeper nesting fails the packet, not just the stack: this server refuses
		// where vanilla's decoder overflows its Java stack, so the reason names the
		// packet rather than a StackOverflow. Other decode errors drop the packet.
		if errors.Is(err, protocol.ErrSlotTooDeep) {
			return c.decodeFailure("set_creative_mode_slot", err)
		}
		return nil
	}

	// vanilla ignores this outside creative; otherwise a modified client could
	// create items.
	if c.player.GameMode() != protocol.GameModeCreative {
		c.log.Debug("ignoring a creative inventory action outside creative mode", "slot", slot)
		return nil
	}
	if ok && !stack.Empty() {
		dropRemovalsWithoutDefault(&stack)
		// An unknown item would be saved and sent as equipment other clients cannot
		// decode.
		if _, known := registry.ItemName(stack.ItemID); !known || stack.ItemID == 0 {
			c.log.Debug("refused an unknown item", "slot", slot, "item", stack.ItemID)
			ok = false
		} else if stack.Count > registry.StackMaxStackSize(stack) {
			// An oversized stack is refused, not trimmed: the client shows what it
			// sent, so the slot resend restores the shown stack.
			c.log.Debug("refused a stack larger than the item's maximum",
				"slot", slot, "item", stack.ItemID, "count", stack.Count)
			ok = false
		}
		// Wear is accepted only below the breaking point; an unbreakable stack
		// never breaks. vanilla: ItemStack.isBroken
		if stack.Damage > 0 && !stack.Unbreakable && stack.Damage >= registry.StackMaxDamage(stack) {
			c.log.Debug("refused a stack with wear an item cannot take", "slot", slot, "item", stack.ItemID)
			ok = false
		}
	}

	// The client already removed the dropped stack; the server throws it,
	// throttled so a client with a full inventory cannot flood the world.
	if slot == -1 {
		if ok && !stack.Empty() {
			if !c.dropSpam.allow(c.spamTick(), dropSpamThreshold) {
				c.log.Warn("refused a creative drop past the drop throttle",
					"player", c.player.Name, "threshold", dropSpamThreshold)
				return nil
			}
			c.throwFromHand(stack, nil)
		}
		return nil
	}

	// Slot 0 is the crafting output; vanilla accepts only the player's slots 1
	// to 45.
	if slot < 1 || !player.ValidSlot(slot) {
		return nil
	}

	if !ok {
		// The stack cannot be held here (unknown item, or wear past the end); the
		// client already shows it, so resend the real slot.
		var was []byte
		c.player.Inventory(func(inv *player.Inventory) {
			was = inv.WindowSlot(playerWindowID, slot, inv.Slots[slot])
		})
		c.log.Debug("refused an item the server cannot hold", "slot", slot)
		return c.writePacket(protocol.IDPlayContainerSlot, "set container slot", was)
	}

	var visible bool
	c.player.Inventory(func(inv *player.Inventory) {
		inv.Set(int(slot), stack)
		visible = slot >= player.ArmorSlot && slot < player.ArmorSlot+player.ArmorSlots ||
			slot == player.OffHandSlot ||
			slot == player.HotbarSlot+inv.Selected
	})
	// Others see armour, the off hand and the selected hotbar slot.
	if visible {
		c.broadcastEquipment()
	}
	return nil
}

// syncTwoSlots sends both slots of a swap, broadcasts equipment and logs
// msg.
func (c *client) syncTwoSlots(a, b int, msg string, args ...any) error {
	var slots [2][]byte
	c.player.Inventory(func(inv *player.Inventory) {
		slots[0] = inv.WindowSlot(playerWindowID, int32(a), inv.Slots[a])
		slots[1] = inv.WindowSlot(playerWindowID, int32(b), inv.Slots[b])
	})
	for _, packet := range slots {
		if err := c.writePacket(protocol.IDPlayContainerSlot, "set container slot", packet); err != nil {
			return err
		}
	}
	c.broadcastEquipment()
	c.log.Debug(msg, args...)
	return nil
}

// equipHeldItem moves a wearable held item to its armour slot and reports
// whether the swap happened.
func (c *client) equipHeldItem(hand int32) (bool, error) {
	var held, worn int
	var equipped bool
	var item int32
	var was, now, stow protocol.ItemStack
	creative := c.player.GameMode() == protocol.GameModeCreative
	c.player.Inventory(func(inv *player.Inventory) {
		held = player.HotbarSlot + int(inv.Selected)
		if hand == protocol.HandOff {
			held = player.OffHandSlot
		}
		item = inv.Slots[held].ItemID
		// Equip leaves the hand holding the item when it wears one of a
		// stack, so the slot's old content is read before.
		if eq, ok := registry.StackEquippable(inv.Slots[held]); ok {
			if slot, ok := player.EquipmentSlotOf(eq.Slot); ok {
				was = inv.Slots[slot]
			}
		}
		worn, stow, equipped = inv.Equip(held, creative)
		if equipped {
			now = inv.Slots[worn]
		}
	})
	if !equipped {
		return false, nil
	}
	// was is what the armour slot held before the equip.
	c.armorChanged(worn, was, now)
	// vanilla: Equippable.swapWithEquipmentSlot
	c.awardItemStat(registry.StatTypeUsed, item, 1)
	if err := c.syncTwoSlots(held, worn, "equipped held item", "from", held, "to", worn); err != nil {
		return true, err
	}
	if !stow.Empty() {
		if left := c.addToInventory(stow); left > 0 {
			stow.Count = left
			c.throwItem(stow)
		}
	}
	return true, nil
}

// swapHands swaps the main and off hand (the F key).
func (c *client) swapHands() error {
	newMain, newOff, ok, edited := c.pluginSwapHands()
	var main, off int
	c.player.Inventory(func(inv *player.Inventory) {
		if !ok {
			// Cancelled: only the slots to resync are needed.
			main, off = player.HotbarSlot+int(inv.Selected), player.OffHandSlot
			return
		}
		main, off = inv.SwapHands()
		if edited {
			inv.Set(main, newMain)
			inv.Set(off, newOff)
		}
	})
	return c.syncTwoSlots(main, off, "swapped hands", "main", main, "off", off)
}

// pickBlock handles middle-click: an item on the hotbar is selected, one in
// the main inventory swaps with suitableHotbarSlot's slot, and only
// creative creates items not carried (survival leaves the hand alone). Set
// Held Item goes on every accepted pick; Set Container Slot for rewritten
// slots, and equipment if the held item changed.
// vanilla: ServerGamePacketListenerImpl.tryPickItem
func (c *client) pickBlock(pick protocol.ItemStack) error {
	var a, b, was, selected int
	var rewrote bool
	// Read the mode before the inventory lock: GameMode takes the lock the
	// closure holds.
	creative := c.player.GameMode() == protocol.GameModeCreative
	c.player.Inventory(func(inv *player.Inventory) {
		was = int(inv.Selected)
		a, b, rewrote = inv.PickStack(pick, creative)
		selected = int(inv.Selected)
	})
	if err := c.writePacket(protocol.IDPlayHeldItem, "set held item", player.HeldItem(int32(selected))); err != nil {
		return err
	}
	if !rewrote {
		// The pick at most moved the selection; others see a different hand when it
		// did.
		if selected != was {
			c.broadcastEquipment()
		}
		return nil
	}
	if b < 0 {
		// A creative conjure rewrote the held slot alone.
		var payload []byte
		c.player.Inventory(func(inv *player.Inventory) {
			payload = inv.WindowSlot(playerWindowID, int32(a), inv.Slots[a])
		})
		if err := c.writePacket(protocol.IDPlayContainerSlot, "set container slot", payload); err != nil {
			return err
		}
		c.broadcastEquipment()
		return nil
	}
	return c.syncTwoSlots(a, b,
		"picked an item into the held slot", "held", a, "from", b)
}

// sendHeldItem puts the player's hotbar selection back where they left it.
func (c *client) sendHeldItem() error {
	var selected int32
	c.player.Inventory(func(inv *player.Inventory) { selected = inv.Selected })
	return c.writePacket(protocol.IDPlayHeldItem, "set held item", player.HeldItem(selected))
}

// dropRemovalsWithoutDefault takes out the removals of components the
// stack's item has no value for, as the decoded patch does in vanilla.
// vanilla: PatchedDataComponentMap.fromPatch
func dropRemovalsWithoutDefault(stack *protocol.ItemStack) {
	if stack.Removed == 0 {
		return
	}
	for _, name := range stack.RemovedNames() {
		if component, ok := protocol.RemovableType(name); ok &&
			registry.ItemLacksComponentDefault(stack.ItemID, component) {
			stack.ResetComponent(component)
		}
	}
}
