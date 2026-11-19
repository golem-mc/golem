package inventory

import (
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/registry"
)

// The stonecutter's part of a click: Menu[0] the input, Menu[1] the stored
// cut of the selected recipe, filled and cleared by internal/server/stonecutter.go. A
// take spends one input; the cut stays on offer until the take that spends
// the last piece, which clears it.
// vanilla: StonecutterMenu

// StonecutterResult is the input's index'th recipe output, in the join's
// Update Recipes order; false when the item has no such recipe.
func StonecutterResult(input, index int32) (protocol.ItemStack, bool) {
	recipe, ok := stonecutterRecipe(input, index)
	if !ok {
		return protocol.ItemStack{}, false
	}
	return protocol.ItemStack{ItemID: recipe.ResultID, Count: recipe.ResultCount}, true
}

func stonecutterRecipe(input, index int32) (registry.StonecutterRecipe, bool) {
	if index < 0 {
		return registry.StonecutterRecipe{}, false
	}
	for _, recipe := range registry.StonecutterRecipesNow() {
		if recipe.Input != input {
			continue
		}
		if index == 0 {
			return recipe, true
		}
		index--
	}
	return registry.StonecutterRecipe{}, false
}

// stonecutterAccepts reports whether some recipe cuts the item, which
// shift-click routing asks; the input slot itself takes anything.
func stonecutterAccepts(stack protocol.ItemStack) bool {
	for _, recipe := range registry.StonecutterRecipesNow() {
		if recipe.Input == stack.ItemID {
			return true
		}
	}
	return false
}

// stonecutterTake awards the selected recipe and spends one input.
// vanilla: StonecutterMenu$2.onTake, the input's change clearing the result
func stonecutterTake(view View, inv *player.Inventory, outcome *Outcome) {
	input := view.Menu[0]
	if input.Empty() {
		return
	}
	// vanilla: StonecutterMenu result slot onTake, ItemStack.onCraftedBy
	if result := view.Menu[StonecutterResultSlot]; !result.Empty() {
		outcome.Crafted = append(outcome.Crafted, result)
	}
	if view.StonecutterRecipe != "" {
		outcome.Recipes = append(outcome.Recipes, newRecipeCraft(view.StonecutterRecipe, view.Menu[:1]))
	}
	input.Count--
	if input.Count <= 0 {
		input = protocol.ItemStack{}
	}
	setStack(view, inv, 0, input, outcome)
	if input.Empty() {
		view.Menu[StonecutterResultSlot] = protocol.ItemStack{}
		outcome.touched(StonecutterResultSlot)
	}
	outcome.StonecutterTaken = true
}

// stonecutterFromResult is mode 0: the whole result onto an empty cursor or
// onto the same item with room for all, either button; otherwise refused.
// One input is spent regardless.
// vanilla: AbstractContainerMenu.doClick PICKUP, Slot.tryRemove
func stonecutterFromResult(s *Session, view View, inv *player.Inventory, outcome *Outcome) {
	stored := view.Menu[StonecutterResultSlot]
	if !takeResult(s, stored, stored) {
		return
	}
	stonecutterTake(view, inv, outcome)
}

// shiftStonecutterFromResult is mode 1: one cut per piece into the player's
// things, hotbar end first, while it lands and the input lasts. A cut that
// lands nothing is not spent; a partial remainder falls at the feet.
// vanilla: StonecutterMenu.quickMoveStack, AbstractContainerMenu.doClick QUICK_MOVE
func shiftStonecutterFromResult(view View, inv *player.Inventory, outcome *Outcome) {
	base := view.InvBase()
	targets := playerSectionSlots(base, base+player.MainInvSlots)
	for {
		result := view.Menu[StonecutterResultSlot]
		if result.Empty() {
			return
		}
		left := moveIntoStacks(view, inv, result, targets, true, outcome)
		if left.Count == result.Count {
			// Nothing went in anywhere, so the quick-move answers
			// nothing and spends no input.
			return
		}
		if left.Count > 0 {
			outcome.Dropped = append(outcome.Dropped, left)
		}
		stonecutterTake(view, inv, outcome)
	}
}

// stonecutterSwap is mode 2: the whole result into an empty hotbar slot,
// spending one input.
// vanilla: AbstractContainerMenu.doClick SWAP
func stonecutterSwap(view View, inv *player.Inventory, button int32, outcome *Outcome) {
	invSlot, ok := swapButtonSlot(button)
	if !ok {
		return
	}
	stored := view.Menu[StonecutterResultSlot]
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
	stonecutterTake(view, inv, outcome)
}

// stonecutterThrow is mode 4: the whole cut drops (removeItem ignores the
// amount), spending one input; the result refills while input remains, so
// Ctrl+Q throws until it runs out.
// vanilla: ResultContainer.removeItem, AbstractContainerMenu.doClick THROW
func stonecutterThrow(view View, inv *player.Inventory, button int32, outcome *Outcome) {
	for {
		stored := view.Menu[StonecutterResultSlot]
		if stored.Empty() {
			return
		}
		stonecutterTake(view, inv, outcome)
		outcome.throw(stored)
		if button == 0 {
			return
		}
	}
}
