package server

import (
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// Compass and lodestone: a compass used on a lodestone becomes a lodestone
// compass pointing at it, the component the client reads; the block itself
// is plain.

// tickLodestoneCompasses ticks every carried compass (mobs' are in
// tickMobCompasses): one whose lodestone is gone (broken, or outside the
// world's bounds) loses its target and spins, keeping the component and the
// tracked flag. One pointing into another dimension is left alone; one at an
// unloaded chunk is judged by the saved chunk.
// vanilla: CompassItem.inventoryTick, LodestoneTracker.tick
func (c *client) tickLodestoneCompasses() {
	w := c.theWorld()
	dim := c.dimension().name
	bottom, top := w.MinY(), w.MinY()+w.Height()
	type slotSync struct {
		payload []byte
		visible bool
	}
	var syncs []slotSync
	probe := &c.compassProbe
	*probe = lodestoneProbe{w: w, misses: probe.misses[:0]}
	// Slot updates are built only on the pass that writes the stacks: a saved
	// chunk is read between the passes with the inventory lock released, and an
	// update built before that would overtake a newer write of the slot.
	// Crafting slots are menu state, not inventory, and are not ticked.
	pass := func(final bool) {
		c.player.Inventory(func(inv *player.Inventory) {
			var lost []int
			for slot := player.ArmorSlot; slot < len(inv.Slots); slot++ {
				// Only a stack with components can carry a tracker.
				if len(inv.Slots[slot].Raw) == 0 {
					continue
				}
				if rules.LodestoneCompassLost(probe, &inv.Slots[slot], dim, bottom, top) {
					lost = append(lost, slot)
				}
			}
			// The first pass defers every write while a target is unknown; the
			// final one applies each loss known by then.
			if !final && len(probe.misses) > 0 {
				return
			}
			for _, slot := range lost {
				payload, visible := c.setSlotUnder(inv, slot, rules.ClearedLodestone(inv.Slots[slot]))
				syncs = append(syncs, slotSync{payload: payload, visible: visible})
			}
		})
	}
	pass(false)
	if len(probe.misses) > 0 {
		probe.resolved = w.ResolveLodestones(probe.misses)
		pass(true)
		probe.resolved.Close()
		probe.resolved = nil
	}
	for _, sync := range syncs {
		c.send(protocol.IDPlayContainerSlot, sync.payload)
		if sync.visible {
			c.broadcastEquipment()
		}
	}
}

// lodestoneProbe is a rules.LodestoneWorld that answers from memory and notes
// each position it could not, so the caller reads them once it holds no lock.
// An unknown position counts as present. resolved holds what an earlier read
// found out, checked against its chunk's version at each use.
type lodestoneProbe struct {
	w        *world.World
	misses   []world.LodestonePos
	resolved *world.LodestoneAnswers
}

func (p *lodestoneProbe) LodestoneCached(x, y, z int32) (present, known bool) {
	if present, known = p.w.LodestoneCached(x, y, z); known {
		return present, true
	}
	pos := world.LodestonePos{X: x, Y: y, Z: z}
	if present, known = p.resolved.Lookup(pos); known {
		return present, true
	}
	p.misses = append(p.misses, pos)
	return true, false
}

// lodestoneMob is a mob whose compass targets in saved chunks were unknown
// when it ticked.
type lodestoneMob struct {
	h      *mobHerd
	w      *world.World
	dim    string
	m      *entity.Mob
	misses []world.LodestonePos
}

// finishMobCompasses reads the saved chunks behind the mobs' compass targets
// with no lock held, then ticks each mob's compasses again with the answers
// under the mob lock, if it is still in its herd.
func (s *Server) finishMobCompasses(mobs []lodestoneMob) {
	if len(mobs) == 0 {
		return
	}
	asked := map[*world.World][]world.LodestonePos{}
	for _, lm := range mobs {
		asked[lm.w] = append(asked[lm.w], lm.misses...)
	}
	answers := make(map[*world.World]*world.LodestoneAnswers, len(asked))
	for w, ps := range asked {
		answers[w] = w.ResolveLodestones(ps)
		defer answers[w].Close()
	}
	s.mobsMu.Lock()
	defer s.mobsMu.Unlock()
	for _, lm := range mobs {
		if lm.h.book[lm.m] == nil {
			continue
		}
		tickMobCompasses(lm.m, &lodestoneProbe{w: lm.w, resolved: answers[lm.w]}, lm.dim)
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
