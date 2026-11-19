package inventory

import (
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
)

// resultSlot is the click handling of a table that spends one of each input
// per take (cartography table, loom). The slot holds the stored offer;
// output is what a take hands over, called only for a take that happens.
type resultSlot struct {
	slot   int32
	output func(*player.Inventory, protocol.ItemStack) protocol.ItemStack
	// take spends the inputs and flags the outcome for the take's sound.
	take func(View, *player.Inventory, *Outcome)
}

// fromResult is mode 0: the whole result onto an empty cursor or the same
// item with room for all.
// vanilla: AbstractContainerMenu.doClick PICKUP, Slot.tryRemove
func (r resultSlot) fromResult(s *Session, view View, inv *player.Inventory, outcome *Outcome) {
	stored := view.Menu[r.slot]
	// takeResult hands given over only onto an empty cursor.
	given := stored
	if s.Carried.Empty() && !stored.Empty() {
		given = r.output(inv, stored)
	}
	if !takeResult(s, stored, given) {
		return
	}
	r.take(view, inv, outcome)
}

// shiftFromResult is mode 1: the output moves into the player's things,
// hotbar end first, once per take, while it lands somewhere and the inputs
// last. What found no room is dropped.
// vanilla: AbstractContainerMenu.doClick QUICK_MOVE, repeating while the
// clicked slot still holds what it moved
func (r resultSlot) shiftFromResult(view View, inv *player.Inventory, outcome *Outcome) {
	base := view.InvBase()
	targets := playerSectionSlots(base, base+player.MainInvSlots)
	for {
		result := view.Menu[r.slot]
		if result.Empty() {
			return
		}
		left := moveIntoStacks(view, inv, r.output(inv, result), targets, true, outcome)
		if left.Count == result.Count {
			return
		}
		if left.Count > 0 {
			outcome.Dropped = append(outcome.Dropped, left)
		}
		r.take(view, inv, outcome)
	}
}

// swap is mode 2: the output into an empty hotbar slot, spending the
// inputs.
// vanilla: AbstractContainerMenu.doClick SWAP
func (r resultSlot) swap(view View, inv *player.Inventory, button int32, outcome *Outcome) {
	invSlot, ok := swapButtonSlot(button)
	if !ok {
		return
	}
	stored := view.Menu[r.slot]
	if stored.Empty() || !inv.Slots[invSlot].Empty() {
		return
	}
	inv.Set(invSlot, r.output(inv, stored))
	outcome.touchedInv(view, invSlot)
	if player.VisibleSlot(inv, invSlot) {
		outcome.Visible = true
	}
	r.take(view, inv, outcome)
}

// throw is mode 4: the whole offer drops, Ctrl+Q repeating until the inputs
// run out.
// vanilla: ResultContainer.removeItem, AbstractContainerMenu.doClick THROW
func (r resultSlot) throw(view View, inv *player.Inventory, button int32, outcome *Outcome) {
	for {
		stored := view.Menu[r.slot]
		if stored.Empty() {
			return
		}
		r.take(view, inv, outcome)
		outcome.throw(r.output(inv, stored))
		if button == 0 {
			return
		}
	}
}
