package inventory

import (
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/rules"
)

// The anvil's part of a click: two inputs and the typed name. The result is
// computed at take time (rules/anvil.go); the take clears the inputs, spends
// the level cost and damages the anvil.
// vanilla: AnvilMenu.onTake, via the result slot's safeTake

// anvilAnswer is what the anvil makes of its two inputs and the name the
// window holds.
func anvilAnswer(view View, inv *player.Inventory, creative bool) rules.AnvilAnswer {
	return rules.AnvilCombine(view.Stack(inv, 0), view.Stack(inv, 1), view.AnvilName, creative)
}

// anvilMayPickup is the anvil result slot's own mayPickup: something the
// anvil makes, and the levels to pay for it unless the player may conjure.
func anvilMayPickup(answer rules.AnvilAnswer, creative bool, xpLevel int32) bool {
	return answer.Cost > 0 && (creative || xpLevel >= answer.Cost)
}

// anvilTake: the first input always goes; the second goes or shrinks by what
// the combine used, except for a rename, which keeps it. The stored result
// is left for the next recompute, which the emptied inputs make nothing.
// vanilla: AnvilMenu.onTake
func anvilTake(view View, inv *player.Inventory, answer rules.AnvilAnswer, outcome *Outcome) {
	setStack(view, inv, 0, protocol.ItemStack{}, outcome)
	setStack(view, inv, 1, rules.AnvilTake(view.Stack(inv, 1), answer), outcome)
	outcome.AnvilTaken = true
	outcome.AnvilCost = answer.Cost
}

// anvilFromResult is mode 0 on the result: the whole result onto an empty
// cursor, or onto the same item with room for all of it, either button;
// otherwise refused.
// vanilla: AbstractContainerMenu.doClick PICKUP, Slot.tryRemove
func anvilFromResult(s *Session, view View, inv *player.Inventory, creative bool, xpLevel int32, outcome *Outcome) {
	answer := anvilAnswer(view, inv, creative)
	if !anvilMayPickup(answer, creative, xpLevel) {
		return
	}
	if !takeResult(s, answer.Result, answer.Result) {
		return
	}
	anvilTake(view, inv, answer, outcome)
}

// shiftAnvilFromResult is mode 1 on the result: one move into the player's
// things, hotbar end first, then the take. A move that placed nothing takes
// nothing (quickMoveStack answering empty); a partial move's remainder is
// overwritten by the recompute.
// vanilla: ItemCombinerMenu.quickMoveStack
func shiftAnvilFromResult(view View, inv *player.Inventory, creative bool, xpLevel int32, outcome *Outcome) {
	answer := anvilAnswer(view, inv, creative)
	if !anvilMayPickup(answer, creative, xpLevel) {
		return
	}
	base := view.InvBase()
	targets := playerSectionSlots(base, base+player.MainInvSlots)
	left := moveIntoStacks(view, inv, answer.Result, targets, true, outcome)
	if left.Count == answer.Result.Count {
		// Nothing went in anywhere, so the quick-move answers nothing
		// and the take never runs.
		return
	}
	anvilTake(view, inv, answer, outcome)
}

// anvilSwap is mode 2 on the result: the whole result into an empty hotbar
// slot, the only direction a swap runs against a never-placing slot.
func anvilSwap(view View, inv *player.Inventory, button int32, creative bool, xpLevel int32, outcome *Outcome) {
	invSlot, ok := swapButtonSlot(button)
	if !ok {
		return
	}
	answer := anvilAnswer(view, inv, creative)
	if !anvilMayPickup(answer, creative, xpLevel) {
		return
	}
	if !inv.Slots[invSlot].Empty() {
		return
	}
	inv.Set(invSlot, answer.Result)
	outcome.touchedInv(view, invSlot)
	if player.VisibleSlot(inv, invSlot) {
		outcome.Visible = true
	}
	anvilTake(view, inv, answer, outcome)
}

// anvilThrow is mode 4 on the result: the whole result drops and the take
// pays; mayPickup is checked first so an unaffordable result never drops.
// removeItem ignores the amount, so Q and Ctrl+Q both drop it whole.
// vanilla: ResultContainer.removeItem, AbstractContainerMenu.doClick THROW
func anvilThrow(view View, inv *player.Inventory, creative bool, xpLevel int32, outcome *Outcome) {
	answer := anvilAnswer(view, inv, creative)
	if !anvilMayPickup(answer, creative, xpLevel) {
		return
	}
	anvilTake(view, inv, answer, outcome)
	outcome.throw(answer.Result)
}

// anvilCollect is a double click gathering the anvil result: with the same
// stack held, affordable and with room for all of it, the take clears and
// pays. A double click on the result itself never gets here; the sweep
// skips the clicked slot.
// vanilla: AbstractContainerMenu.doClick PICKUP_ALL, Slot.tryRemove
func anvilCollect(session *Session, view View, inv *player.Inventory, creative bool, xpLevel int32, outcome *Outcome) {
	held := session.Carried
	if held.Empty() {
		// The vanilla collect gathers only with something on the cursor
		// to gather beside.
		return
	}
	answer := anvilAnswer(view, inv, creative)
	if !anvilMayPickup(answer, creative, xpLevel) {
		return
	}
	result := view.Stack(inv, AnvilResultSlot)
	if !sameStack(held, result) {
		// canItemQuickReplace wants the same item and components.
		return
	}
	if stackMax(held)-held.Count < result.Count {
		// The result slot cannot be placed into, so its take is the whole
		// result or nothing.
		return
	}
	anvilTake(view, inv, answer, outcome)
	held.Count += result.Count
	session.Carried = held
}

// RecomputeAnvil recomputes Menu[2] from the inputs and typed name, storing
// it when it differs or force is set; stored reports which.
// vanilla: AnvilMenu.createResult
func RecomputeAnvil(menu []protocol.ItemStack, name string, creative, force bool) (cost int32, stored bool) {
	answer := rules.AnvilCombine(menu[0], menu[1], name, creative)
	if force || !answer.Result.Equal(menu[2]) {
		menu[2] = answer.Result
		return answer.Cost, true
	}
	return answer.Cost, false
}
