package inventory

import (
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/registry"
)

// The smithing table's part of a click: Menu[0] template, Menu[1] base,
// Menu[2] addition, Menu[3] the stored result, recomputed here and by
// internal/server/smithing.go's input hooks. A take spends one of each input and the
// rest refill the result.
// vanilla: SmithingMenu

// RecomputeSmithing recomputes Menu[3] from the inputs, storing it when it
// differs; stored reports which.
// vanilla: SmithingMenu.createResult
func RecomputeSmithing(menu []protocol.ItemStack) (stored bool) {
	result := registry.SmithingResult(menu[0], menu[1], menu[2])
	if result.Equal(menu[SmithingResultSlot]) {
		return false
	}
	menu[SmithingResultSlot] = result
	return true
}

// smithingTake spends one of each input and recomputes; emptied inputs, or a
// trim the base already wears, leave it empty.
// vanilla: SmithingMenu.onTake, the recompute the spent inputs trigger
func smithingTake(view View, inv *player.Inventory, outcome *Outcome) {
	if id, _ := registry.SmithingMatch(view.Menu[0], view.Menu[1], view.Menu[2]); id != "" {
		craft := newRecipeCraft(id, view.Menu[:SmithingResultSlot])
		outcome.Recipes = append(outcome.Recipes, craft)
	}
	// vanilla: SmithingMenu.onTake, ItemStack.onCraftedBy
	if result := view.Menu[SmithingResultSlot]; !result.Empty() {
		outcome.Crafted = append(outcome.Crafted, result)
	}
	for slot := range int32(SmithingResultSlot) {
		input := view.Stack(inv, slot)
		if input.Empty() {
			continue
		}
		input.Count--
		if input.Count <= 0 {
			input = protocol.ItemStack{}
		}
		setStack(view, inv, slot, input, outcome)
	}
	if stored := RecomputeSmithing(view.Menu); stored {
		outcome.touched(SmithingResultSlot)
	}
	outcome.SmithingTaken = true
}

// smithingFromResult is mode 0: the whole result onto an empty cursor or
// onto the same item with room for all, either button; otherwise refused.
// One of each input is spent regardless.
// vanilla: AbstractContainerMenu.doClick PICKUP, Slot.tryRemove
func smithingFromResult(s *Session, view View, inv *player.Inventory, outcome *Outcome) {
	stored := view.Menu[SmithingResultSlot]
	if !takeResult(s, stored, stored) {
		return
	}
	smithingTake(view, inv, outcome)
}

// shiftSmithingFromResult is mode 1: the result moves into the player's
// things, hotbar end first, repeating while it lands and the inputs last.
// A result with no room stays put.
// vanilla: ItemCombinerMenu.quickMoveStack, the QUICK_MOVE that repeats
// while the clicked slot still holds what it moved
func shiftSmithingFromResult(view View, inv *player.Inventory, outcome *Outcome) {
	base := view.InvBase()
	targets := playerSectionSlots(base, base+player.MainInvSlots)
	for {
		result := view.Menu[SmithingResultSlot]
		if result.Empty() {
			return
		}
		left := moveIntoStacks(view, inv, result, targets, true, outcome)
		if left.Count == result.Count {
			// Nothing went in anywhere, so the quick-move answers
			// nothing and spends no inputs.
			return
		}
		smithingTake(view, inv, outcome)
	}
}

// smithingSwap is mode 2: the whole result into an empty hotbar slot,
// spending one of each input.
// vanilla: AbstractContainerMenu.doClick SWAP
func smithingSwap(view View, inv *player.Inventory, button int32, outcome *Outcome) {
	invSlot, ok := swapButtonSlot(button)
	if !ok {
		return
	}
	stored := view.Menu[SmithingResultSlot]
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
	smithingTake(view, inv, outcome)
}

// smithingThrow is mode 4: the result (one item, removeItem ignoring the
// amount anyway) drops, spending one of each input; Ctrl+Q repeats until
// the inputs run out.
// vanilla: ResultContainer.removeItem, AbstractContainerMenu.doClick THROW
func smithingThrow(view View, inv *player.Inventory, button int32, outcome *Outcome) {
	for {
		stored := view.Menu[SmithingResultSlot]
		if stored.Empty() {
			return
		}
		smithingTake(view, inv, outcome)
		outcome.throw(stored)
		if button == 0 {
			return
		}
	}
}

// smithingAccepts reports whether an empty input takes the stack's kind:
// template, base or addition by its property set.
// vanilla: SmithingMenu.canMoveIntoInputSlots
func smithingAccepts(view View, inv *player.Inventory, stack protocol.ItemStack) bool {
	for slot, set := range []string{smithingTemplateSet, smithingBaseSet, smithingAdditionSet} {
		if registry.ItemInPropertySet(set, stack.ItemID) && view.Stack(inv, int32(slot)).Empty() {
			return true
		}
	}
	return false
}
