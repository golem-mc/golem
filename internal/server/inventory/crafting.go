package inventory

import (
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/registry"
)

// Crafting results: taking one consumes one of each used ingredient (every
// non-empty grid cell), returning crafting remainders to their cells. What a
// grid is worth is registry.CraftResult's (registry/recipes.go).

// craftResultOf is the window's crafting result, empty when no recipe
// matches. The inventory lock must be held: the player's own grid is four of
// its slots.
func craftResultOf(view View, inv *player.Inventory) protocol.ItemStack {
	cells, width, ok := view.gridOf(inv)
	if !ok {
		return protocol.ItemStack{}
	}
	if view.Kind == KindCrafter {
		return registry.CraftResult(cells, width)
	}
	_, result := inv.CraftMatch(cells, width)
	return result
}

// craftFromResult is the plain click on the result: the cursor must be
// empty, or the same item with room for one craft.
func craftFromResult(session *Session, view View, inv *player.Inventory, button int32, outcome *Outcome) {
	if view.Kind == KindCrafter {
		// The crafter's result slot only shows its grid's result; crafting
		// happens on a redstone pulse.
		// vanilla: NonInteractiveResultSlot
		return
	}
	cells, width, ok := view.gridOf(inv)
	if !ok {
		return
	}

	id, result := inv.CraftMatch(cells, width)
	if result.Empty() {
		return
	}
	held := session.Carried
	wasEmpty := held.Empty()
	switch {
	case held.Empty():
		result = inv.TakeCraft(result)
		session.Carried = result
	case sameStack(held, result) && held.Count+result.Count <= registry.StackMaxStackSize(result):
		session.Carried.Count += result.Count
	default:
		return
	}
	// Right-clicking with an empty cursor asks for half, rounded up, and that
	// is what counts though the whole result lands on the cursor; a matching
	// held stack takes the whole result either way.
	// vanilla: AbstractContainerMenu.doClick PICKUP, ResultSlot.remove
	counted := result
	if button == 1 && wasEmpty {
		counted.Count = (result.Count + 1) / 2
	}
	outcome.Crafted = append(outcome.Crafted, counted)
	consumeGrid(view, inv, cells, id, outcome)
}

// craftSwap is mode 2 on the result: crafts into the keyed (or F) slot if
// empty; otherwise nothing.
// vanilla: AbstractContainerMenu.doClick SWAP, ResultSlot.onTake
func craftSwap(view View, inv *player.Inventory, button int32, outcome *Outcome) {
	if view.Kind == KindCrafter {
		return
	}
	invSlot, ok := swapButtonSlot(button)
	if !ok {
		return
	}
	if !inv.Slots[invSlot].Empty() {
		return
	}
	cells, width, ok := view.gridOf(inv)
	if !ok {
		return
	}
	id, result := inv.CraftMatch(cells, width)
	if result.Empty() {
		return
	}
	result = inv.TakeCraft(result)
	inv.Set(invSlot, result)
	outcome.touchedInv(view, invSlot)
	if player.VisibleSlot(inv, invSlot) {
		outcome.Visible = true
	}
	outcome.Crafted = append(outcome.Crafted, result)
	consumeGrid(view, inv, cells, id, outcome)
}

// shiftCraftFromResult crafts as many as the ingredients and room allow, one
// craft at a time while the grid keeps making the same item. Each output is
// quick-moved like vanilla's menus, onto partial stacks then into the first
// empty slot, both from the far end (hotbar right to left, then main rows
// bottom up), matching the client's prediction. A craft that fits nowhere is
// not spent (vanilla's quick-move returns before the take); remainders fall
// at the player's feet.
func shiftCraftFromResult(view View, inv *player.Inventory, outcome *Outcome) {
	if view.Kind == KindCrafter {
		return
	}
	cells, width, ok := view.gridOf(inv)
	if !ok {
		return
	}
	base := view.InvBase()
	targets := playerSectionSlots(base, base+player.MainInvSlots)
	for {
		id, result := inv.CraftMatch(cells, width)
		if result.Empty() {
			return
		}
		result = inv.TakeCraft(result)
		left := moveIntoStacks(view, inv, result, targets, true, outcome)
		if left.Count == result.Count {
			// Nothing fit: quick-move answers nothing and the loop ends with the
			// grid unchanged.
			return
		}
		// Only what the quick-move moved counts.
		// vanilla: Slot.onQuickCraft
		moved := result
		moved.Count -= left.Count
		outcome.Crafted = append(outcome.Crafted, moved)
		consumeGrid(view, inv, cells, id, outcome)
		if left.Count > 0 {
			outcome.Dropped = append(outcome.Dropped, left)
		}
		// The repeat runs while the grid is worth the same item again -
		// the result slot the vanilla loop re-reads between its crafts.
		_, next := inv.CraftMatch(cells, width)
		if next.Empty() || next.ItemID != result.ItemID {
			return
		}
	}
}

// consumeGrid spends one ingredient from every non-empty cell and announces
// the cells and the result slot. Crafting remainders go into the emptied
// cell, else the player's things, else the ground. cells is the grid itself
// (see View.gridOf).
// vanilla: ResultSlot.onTake
func consumeGrid(view View, inv *player.Inventory, cells []protocol.ItemStack, id string, outcome *Outcome) {
	if id != "" {
		outcome.Recipes = append(outcome.Recipes, newRecipeCraft(id, cells))
	}
	remainders := registry.CraftRemainders(cells)
	for i := range cells {
		if cells[i].Empty() {
			continue
		}
		cells[i].Count--
		if cells[i].Count <= 0 {
			cells[i] = protocol.ItemStack{}
		}
		remainder := remainders[i]
		if remainder.Empty() {
			continue
		}
		if cells[i].Empty() {
			cells[i] = remainder
			continue
		}
		if cells[i].Same(remainder) {
			cells[i].Count += remainder.Count
			continue
		}
		left, landed := inv.Add(remainder)
		for _, invSlot := range landed {
			outcome.touchedInv(view, invSlot)
			if player.VisibleSlot(inv, invSlot) {
				outcome.Visible = true
			}
		}
		if left > 0 {
			spill := remainder
			spill.Count = left
			outcome.Dropped = append(outcome.Dropped, spill)
		}
	}
	from, slots := int32(playerCraftingGrid), int32(playerGridSlots)
	if view.Kind == KindCrafting {
		from, slots = craftingTableGrid, CraftingGridCells
	}
	for slot := from; slot < from+slots; slot++ {
		outcome.touched(slot)
	}
	outcome.touched(0)
}
