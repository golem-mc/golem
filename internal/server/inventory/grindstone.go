package inventory

import (
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/registry"
)

// The grindstone's part of a click: Menu[0] and Menu[1] the inputs,
// Menu[GrindstoneResultSlot] the last recompute (internal/server/containers.go). A
// take empties the inputs and reports the stripped enchantments' worth for
// the caller to drop as orbs.
// vanilla: GrindstoneMenu

// grindable reports whether the inputs take the stack: damageable, or
// enchanted in either component.
// vanilla: ItemStack.isDamageableItem, EnchantmentHelper.hasAnyEnchantments
func grindable(stack protocol.ItemStack) bool {
	return (registry.StackMaxDamage(stack) > 0 && !stack.Unbreakable) ||
		len(stack.Enchantments) > 0 || len(stack.StoredEnchantments) > 0
}

// grindstoneTake reports the inputs' stripped worth, empties them and clears
// the result, so a later take in the same click finds nothing (safeTake
// empties the slot's container itself).
// vanilla: GrindstoneMenu$3.onTake, Slot.safeTake
func grindstoneTake(view View, inv *player.Inventory, outcome *Outcome) {
	outcome.GrindstoneWorth += rules.GrindstoneWorth(view.Stack(inv, 0)) +
		rules.GrindstoneWorth(view.Stack(inv, 1))
	view.Menu[GrindstoneResultSlot] = protocol.ItemStack{}
	outcome.touched(GrindstoneResultSlot)
	setStack(view, inv, 0, protocol.ItemStack{}, outcome)
	setStack(view, inv, 1, protocol.ItemStack{}, outcome)
	outcome.GrindstoneTaken = true
}

// grindstoneFromResult is mode 0: the whole result onto an empty cursor, or
// onto the same item with room for all, either button; otherwise refused.
// The take empties the inputs regardless.
// vanilla: AbstractContainerMenu.doClick PICKUP, Slot.tryRemove
func grindstoneFromResult(s *Session, view View, inv *player.Inventory, outcome *Outcome) {
	stored := view.Menu[GrindstoneResultSlot]
	if !takeResult(s, stored, stored) {
		return
	}
	grindstoneTake(view, inv, outcome)
}

// shiftGrindstoneFromResult is mode 1: one move into the player's things,
// hotbar end first, then the take. The remainder is overwritten by the
// recompute, which the empty inputs make nothing.
// vanilla: GrindstoneMenu.quickMoveStack, AbstractContainerMenu.doClick QUICK_MOVE
func shiftGrindstoneFromResult(view View, inv *player.Inventory, outcome *Outcome) {
	stored := view.Menu[GrindstoneResultSlot]
	if stored.Empty() {
		return
	}
	base := view.InvBase()
	targets := playerSectionSlots(base, base+player.MainInvSlots)
	left := moveIntoStacks(view, inv, stored, targets, true, outcome)
	if left.Count == stored.Count {
		// Nothing went in anywhere, so the quick-move answers
		// nothing and the take never runs.
		return
	}
	grindstoneTake(view, inv, outcome)
}

// grindstoneSwap is mode 2: the whole result into an empty hotbar slot
// only, then the take.
// vanilla: AbstractContainerMenu.doClick SWAP
func grindstoneSwap(view View, inv *player.Inventory, button int32, outcome *Outcome) {
	invSlot, ok := swapButtonSlot(button)
	if !ok {
		return
	}
	stored := view.Menu[GrindstoneResultSlot]
	if stored.Empty() {
		return
	}
	if !inv.Slots[invSlot].Empty() {
		return
	}
	inv.Set(invSlot, stored)
	outcome.touchedInv(view, invSlot)
	if player.VisibleSlot(inv, invSlot) {
		outcome.Visible = true
	}
	grindstoneTake(view, inv, outcome)
}

// grindstoneThrow is mode 4: the whole result drops for Q and Ctrl+Q alike,
// then the take.
// vanilla: ResultContainer.removeItem
func grindstoneThrow(view View, inv *player.Inventory, outcome *Outcome) {
	stored := view.Menu[GrindstoneResultSlot]
	if stored.Empty() {
		return
	}
	grindstoneTake(view, inv, outcome)
	outcome.throw(stored)
}

// grindstoneCollect is a double click gathering the result: with the same
// item held and room for all, it takes. A double click on the result itself
// never gets here; the click logic's gate skips a clicked slot that still
// holds a result.
// vanilla: AbstractContainerMenu.doClick PICKUP_ALL, Slot.tryRemove
func grindstoneCollect(s *Session, view View, inv *player.Inventory, outcome *Outcome) {
	held := s.Carried
	if held.Empty() {
		// The vanilla collect gathers only with something on the cursor
		// to gather beside.
		return
	}
	result := view.Menu[GrindstoneResultSlot]
	if !sameStack(held, result) {
		// The sweep's canItemQuickReplace wants the same item and
		// components.
		return
	}
	if stackMax(held)-held.Count < result.Count {
		// The result slot cannot be placed into, so its tryRemove
		// refuses a part-take.
		return
	}
	held.Count += result.Count
	s.Carried = held
	grindstoneTake(view, inv, outcome)
}

// RecomputeGrindstone recomputes the result from the two inputs, storing it
// when it differs or force is set; stored reports which.
// vanilla: GrindstoneMenu.createResult
func RecomputeGrindstone(menu []protocol.ItemStack, force bool) (stored bool) {
	result := rules.GrindstoneCombine(menu[0], menu[1])
	if force || !result.Equal(menu[GrindstoneResultSlot]) {
		menu[GrindstoneResultSlot] = result
		return true
	}
	return false
}
