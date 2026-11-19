package server

import (
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/registry"
)

// Compass and lodestone: a compass used on a lodestone becomes a lodestone
// compass pointing at it, the component the client reads; the block itself
// is plain.

// tickLodestoneCompasses ticks every carried compass (mobs' are in
// tickMobCompasses): one whose lodestone is gone (broken, or outside the
// world's bounds) loses its target and spins, keeping the component and the
// tracked flag. One pointing into another dimension, or at a chunk with no
// reading here, is left alone, since neither can be confirmed not a
// lodestone. vanilla: CompassItem.inventoryTick, LodestoneTracker.tick
func (c *client) tickLodestoneCompasses() {
	w := c.theWorld()
	dim := c.dimension().name
	bottom, top := w.MinY(), w.MinY()+w.Height()
	type slotSync struct {
		payload []byte
		visible bool
	}
	var syncs []slotSync
	c.player.Inventory(func(inv *player.Inventory) {
		for slot := range inv.Slots {
			// Only a stack with components can carry a tracker; skipping the rest
			// avoids copying each ItemStack by value.
			if len(inv.Slots[slot].Raw) == 0 {
				continue
			}
			stack, lost := rules.ClearLostLodestone(w, inv.Slots[slot], dim, bottom, top)
			if !lost {
				continue
			}
			payload, visible := c.setSlotUnder(inv, slot, stack)
			syncs = append(syncs, slotSync{payload: payload, visible: visible})
		}
	})
	for _, sync := range syncs {
		c.send(protocol.IDPlayContainerSlot, sync.payload)
		if sync.visible {
			c.broadcastEquipment()
		}
	}
}

// lockLodestone handles a compass used on a lodestone, reporting whether it
// did: the lock sound plays for all, and the compass becomes a lodestone
// compass, in place when it is the only one in a non-creative hand, split
// off as a new stack otherwise. vanilla: CompassItem.useOn
func (c *client) lockLodestone(hand, x, y, z int32) (bool, error) {
	held := c.heldItem(hand)
	if held.Empty() || held.ItemID != int32(registry.ItemCompass) {
		return false, nil
	}
	w := c.theWorld()
	state := w.BlockAt(x, y, z)
	if registry.BlockOf(state) != registry.BlockLodestone {
		return false, nil
	}
	c.srv.broadcastSoundID(w,
		registry.SoundItemLodestoneCompassLock, protocol.SoundCategoryPlayer,
		float64(x)+0.5, float64(y)+0.5, float64(z)+0.5, 1.0, 1.0)

	tracker := protocol.LodestoneTracker{
		Target:  &protocol.LodestoneTarget{Dimension: c.dimension().name, X: x, Y: y, Z: z},
		Tracked: true,
	}
	c.awardUsedHeld(hand)
	// The consume the split spends below is a no-op for the infinite ones.
	if rules.LodestoneLocksInPlace(c.player.GameMode() == protocol.GameModeCreative, held.Count) {
		var payload []byte
		var visible bool
		c.player.Inventory(func(inv *player.Inventory) {
			slot := rules.SlotOfHand(inv, hand)
			held := inv.Slots[slot]
			held.SetLodestoneTracker(tracker)
			payload, visible = c.setSlotUnder(inv, slot, held)
		})
		c.send(protocol.IDPlayContainerSlot, payload)
		if visible {
			c.broadcastEquipment()
		}
		return true, nil
	}
	locked := held
	locked.Count = 1
	locked.SetLodestoneTracker(tracker)
	if c.player.GameMode() != protocol.GameModeCreative {
		if err := c.consumeHeld(hand); err != nil {
			return true, err
		}
	}
	if left := c.addToInventory(locked); left > 0 {
		leftover := locked
		leftover.Count = left
		c.throwItem(leftover)
	}
	return true, nil
}
