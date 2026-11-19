package inventory

import (
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/registry"
)

// Click Container's modes applied to the open window; the packet layout and
// its corrections are internal/server/containers.go's.

// Click applies the click's mode to the player's inventory (and the
// container's slots, locked by the caller); the caller sends once unlocked.
//
// creative and xpLevel are snapshots taken before the inventory lock: the
// accessors take the player's lock, which callbacks here already run under.
// They decide the creative clone and whether the anvil cost is payable.
//
// It reports false for a mode no window serves, the outcome empty, so the
// caller can log the drop. The caller restates the cursor either way.
func (s *Session) Click(view View, inv *player.Inventory, mode, button, slot int32, creative bool, xpLevel int32, outcome *Outcome) bool {
	if !view.Kind.Cooking() {
		return s.click(view, inv, mode, button, slot, creative, xpLevel, outcome)
	}
	// The output only shrinks during a click, so the drop in its count is what
	// the click took, by any mode.
	before := view.Stack(inv, furnaceResultSlot)
	ok := s.click(view, inv, mode, button, slot, creative, xpLevel, outcome)
	if after := view.Stack(inv, furnaceResultSlot); !before.Empty() && outcome.ResultTaken {
		taken := before
		if after.ItemID == before.ItemID {
			taken.Count -= after.Count
		}
		if taken.Count > 0 {
			outcome.Crafted = append(outcome.Crafted, taken)
		}
	}
	return ok
}

func (s *Session) click(view View, inv *player.Inventory, mode, button, slot int32, creative bool, xpLevel int32, outcome *Outcome) bool {
	// A click that is not part of a drag ends any drag in progress, since
	// the paint packets arrive start, marks, end and nothing between.
	s.CancelDrag()

	switch mode {
	case protocol.ClickModePickup:
		if slot == -999 {
			// A click outside the window drops what the cursor holds:
			// the stack for a left click, one of it for a right click.
			if s.Carried.Empty() {
				return true
			}
			drop := s.Carried
			if button == 1 {
				drop.Count = 1
			}
			s.Carried.Count -= drop.Count
			if s.Carried.Count <= 0 {
				s.Carried = protocol.ItemStack{}
			}
			outcome.throw(drop)
			return true
		}
		// A background click (slot -1) is a click on nothing: vanilla returns
		// for any slot below zero but the -999 outside. Otherwise the empty
		// stack a missing slot reads as would take the cursor's stack and
		// lose it.
		if !view.addresses(slot) {
			return true
		}
		clickPickup(s, view, inv, button, slot, creative, xpLevel, outcome)

	case protocol.ClickModeQuickMove:
		if !view.addresses(slot) {
			return true
		}
		quickMove(view, inv, slot, creative, xpLevel, outcome)

	case protocol.ClickModeSwap:
		if !view.addresses(slot) {
			// Vanilla would crash indexing a slot it lacks; a click on nothing
			// trades with nothing.
			return true
		}
		hotbarSwap(view, inv, button, slot, creative, xpLevel, outcome)

	case protocol.ClickModeClone:
		// Middle click clones a whole stack, components included (vanilla:
		// Slot.safeClone), for a creative player only; the cursor keeps what
		// it holds.
		if !creative || !s.Carried.Empty() {
			return true
		}
		if stack := view.Stack(inv, slot); !stack.Empty() {
			clone := stack
			clone.Count = stackMax(stack)
			// The cartography result's mark is the table's, not the
			// map's: vanilla's safeClone post-processes it as onTake does.
			if view.isCartographyResult(slot) {
				clone = inv.TakeCraft(clone)
			}
			s.Carried = clone
		}

	case protocol.ClickModeThrow:
		throwFromSlot(s, view, inv, button, slot, creative, xpLevel, outcome)

	case protocol.ClickModeCollect:
		// The vanilla collect gathers only from a slot the window
		// names; a double click on the background gathers nothing.
		if !view.addresses(slot) {
			return true
		}
		collectStack(s, view, inv, slot, button, creative, xpLevel, outcome)

	default:
		return false
	}
	return true
}

// stackMax is how many of a stack's item one slot holds.
func stackMax(stack protocol.ItemStack) int32 {
	return registry.StackMaxStackSize(stack)
}

// sameStack is the merge condition two stacks must meet: the same item
// with the same components - vanilla's isSameItemSameComponents.
func sameStack(stack, into protocol.ItemStack) bool {
	return stack.Same(into)
}

// takeResult moves a result container's whole stored stack onto the cursor
// (empty, or the same item with room for all) and reports whether it moved;
// removeItem ignores the amount, so a right click takes as much as a left.
// given is what is handed over where it differs from the stored offer (the
// cartography table's map without its mark).
// vanilla: AbstractContainerMenu.doClick PICKUP, Slot.tryRemove,
// ResultContainer.removeItem
func takeResult(s *Session, stored, given protocol.ItemStack) bool {
	if stored.Empty() {
		return false
	}
	held := s.Carried
	if held.Empty() {
		s.Carried = given
		return true
	}
	if !sameStack(held, stored) || stackMax(held)-held.Count < stored.Count {
		return false
	}
	s.Carried.Count += stored.Count
	return true
}

// clickPickup is mode 0, left and right click: pick up, put down, swap and
// halve. A slot that refuses placement draws its stack onto the cursor
// instead, whole or not at all.
func clickPickup(s *Session, view View, inv *player.Inventory, button, slot int32, creative bool, xpLevel int32, outcome *Outcome) {
	if view.isAnvilResult(slot) {
		anvilFromResult(s, view, inv, creative, xpLevel, outcome)
		return
	}
	if view.isStonecutterResult(slot) {
		stonecutterFromResult(s, view, inv, outcome)
		return
	}
	if view.isSmithingResult(slot) {
		smithingFromResult(s, view, inv, outcome)
		return
	}
	if view.isGrindstoneResult(slot) {
		grindstoneFromResult(s, view, inv, outcome)
		return
	}
	if view.isCartographyResult(slot) {
		cartographyResultSlot.fromResult(s, view, inv, outcome)
		return
	}
	if view.isLoomResult(slot) {
		loomResultSlot.fromResult(s, view, inv, outcome)
		return
	}
	if view.isMerchantResult(slot) {
		merchantFromResult(s, view, inv, outcome)
		return
	}
	if view.isResult(slot) {
		craftFromResult(s, view, inv, button, outcome)
		return
	}
	// Bundles take over before the plain swap: a cursor bundle fills from or
	// empties into the slot, a slot bundle takes the cursor or gives back.
	if bundleStackedOnOther(s, view, inv, button, slot, creative, xpLevel, outcome) ||
		bundleOtherStackedOnMe(s, view, inv, button, slot, creative, xpLevel, outcome) {
		return
	}

	stack := view.Stack(inv, slot)
	held := s.Carried
	if boundArmour(view, inv, slot, creative) {
		return
	}
	switch {
	case held.Empty() && button == 0:
		if stack.Empty() {
			return
		}
		setStack(view, inv, slot, protocol.ItemStack{}, outcome)
		s.Carried = stack
		outcome.tookFurnaceResult(view, slot)

	case held.Empty(): // button == 1
		// Right click takes the larger half, the slot keeping the floor;
		// both halves keep the wear.
		if stack.Empty() {
			return
		}
		take := (stack.Count + 1) / 2
		left := stack
		left.Count = stack.Count - take
		setStack(view, inv, slot, left, outcome)
		taken := stack
		taken.Count = take
		s.Carried = taken
		outcome.tookFurnaceResult(view, slot)

	case button == 0:
		// Left click with a held stack: place it, top up, or trade. A
		// slot that takes nothing neither tops up nor trades; the same
		// item held draws the slot's stack onto it instead.
		switch {
		case stack.Empty():
			if view.placeable(slot, held) {
				// The held stack goes in up to the slot's limit (one for the
				// enchanting item slot), the rest staying held.
				// vanilla: Slot.safeInsert
				n := min(held.Count, view.slotLimit(slot, held))
				placed := held
				placed.Count = n
				setStack(view, inv, slot, placed, outcome)
				held.Count -= n
				if held.Count <= 0 {
					held = protocol.ItemStack{}
				}
				s.Carried = held
			}
		case view.placeable(slot, held) && sameStack(stack, held) && stack.Count < view.slotLimit(slot, stack):
			n := min(view.slotLimit(slot, stack)-stack.Count, held.Count)
			stack.Count += n
			setStack(view, inv, slot, stack, outcome)
			s.Carried.Count -= n
			if s.Carried.Count <= 0 {
				s.Carried = protocol.ItemStack{}
			}
		case !view.placeable(slot, held) && sameStack(stack, held):
			// A non-placing slot (furnace output) with its item held: the
			// whole stack comes off or nothing; the take pays the smelts.
			n := stack.Count
			if stackMax(stack)-held.Count < n {
				// vanilla: Slot.tryRemove refuses a partial take.
				return
			}
			left := stack
			left.Count = stack.Count - n
			if left.Count <= 0 {
				left = protocol.ItemStack{}
			}
			setStack(view, inv, slot, left, outcome)
			s.Carried.Count += n
			outcome.tookFurnaceResult(view, slot)
		case !sameStack(stack, held) && view.placeable(slot, held) && held.Count <= view.slotLimit(slot, held):
			// A different stack: trade places, if the held one fits.
			setStack(view, inv, slot, held, outcome)
			s.Carried = stack
		}

	default: // button == 1, a right click with the cursor holding something
		switch {
		case stack.Empty():
			if view.placeable(slot, held) {
				one := held
				one.Count = 1
				setStack(view, inv, slot, one, outcome)
				s.Carried.Count--
				if s.Carried.Count <= 0 {
					s.Carried = protocol.ItemStack{}
				}
			}
		case view.placeable(slot, held) && sameStack(stack, held) && stack.Count < view.slotLimit(slot, stack):
			stack.Count++
			setStack(view, inv, slot, stack, outcome)
			s.Carried.Count--
			if s.Carried.Count <= 0 {
				s.Carried = protocol.ItemStack{}
			}
		case !view.placeable(slot, held) && sameStack(stack, held):
			// Furnace output with its item held: the same take as the left
			// click, vanilla drawing from the slot either way.
			n := stack.Count
			if stackMax(stack)-held.Count < n {
				// vanilla: Slot.tryRemove refuses a partial take.
				return
			}
			left := stack
			left.Count = stack.Count - n
			if left.Count <= 0 {
				left = protocol.ItemStack{}
			}
			setStack(view, inv, slot, left, outcome)
			s.Carried.Count += n
			outcome.tookFurnaceResult(view, slot)
		case !sameStack(stack, held) && view.placeable(slot, held) && held.Count <= view.slotLimit(slot, held):
			// A different stack: trade places, as the left click does.
			setStack(view, inv, slot, held, outcome)
			s.Carried = stack
		}
	}
}

// SetStack writes what a window slot holds from outside a click, appending
// the slots whose stacks changed (the crafting result among them) to
// outcome.Announce.
func (v View) SetStack(inv *player.Inventory, slot int32, stack protocol.ItemStack, outcome *Outcome) {
	setStack(v, inv, slot, stack, outcome)
}

// setStack writes what a window slot holds, into whichever store it lives
// in, and records that the slot changed.
func setStack(view View, inv *player.Inventory, slot int32, stack protocol.ItemStack, outcome *Outcome) {
	loc, index := view.resolve(slot)
	if view.Kind == KindBrewingStand && slot < brewingBottleSlots {
		outcome.replaced = view.Stack(inv, slot)
	}
	switch loc {
	case slotInv:
		inv.Set(index, stack)
		outcome.touched(slot)
		if player.VisibleSlot(inv, index) {
			outcome.Visible = true
		}
	case slotGrid:
		view.Grid[index] = stack
		outcome.touched(slot)
	case slotMenu:
		view.Menu[index] = stack
		outcome.touched(slot)
	case slotEntity:
		view.Entity.Put(index, stack)
		outcome.touched(slot)
		outcome.EntitySlots = append(outcome.EntitySlots, slot)
	}
	// A crafting window's result follows the grid, so the result slot
	// is announced whenever a grid cell changed.
	switch {
	case view.Kind == KindPlayer && loc == slotInv &&
		index >= playerCraftingGrid && index < playerCraftingGrid+playerGridSlots:
		outcome.touched(playerCraftingResult)
	case view.Kind == KindCrafting && loc == slotGrid:
		outcome.touched(craftingTableResult)
	case view.Kind == KindCrafter && loc == slotGrid:
		outcome.touched(view.InvBase() + player.MainInvSlots)
	}
}

// throwFromSlot is mode 4, Q: one item, or the stack for Ctrl+Q. A result
// container's removeItem ignores the amount, so anvil, stonecutter,
// smithing, loom and cartography results drop whole either way; a crafting
// result (not stored, its throw a take) throws one craft, repeated for
// Ctrl+Q. Vanilla's THROW runs only with an empty cursor. A furnace output's
// drop is the take that pays its stored experience.
func throwFromSlot(s *Session, view View, inv *player.Inventory, button, slot int32, creative bool, xpLevel int32, outcome *Outcome) {
	if !s.Carried.Empty() {
		return
	}
	if view.isAnvilResult(slot) {
		anvilThrow(view, inv, creative, xpLevel, outcome)
		return
	}
	if view.isStonecutterResult(slot) {
		stonecutterThrow(view, inv, button, outcome)
		return
	}
	if view.isSmithingResult(slot) {
		smithingThrow(view, inv, button, outcome)
		return
	}
	if view.isGrindstoneResult(slot) {
		grindstoneThrow(view, inv, outcome)
		return
	}
	if view.isCartographyResult(slot) {
		cartographyResultSlot.throw(view, inv, button, outcome)
		return
	}
	if view.isLoomResult(slot) {
		loomResultSlot.throw(view, inv, button, outcome)
		return
	}
	if view.isMerchantResult(slot) {
		merchantThrow(view, inv, button, outcome)
		return
	}
	if view.isResult(slot) {
		throwResult(view, inv, button, outcome)
		return
	}
	stack := view.Stack(inv, slot)
	if stack.Empty() || boundArmour(view, inv, slot, creative) {
		return
	}
	drop := stack
	if button == 0 {
		drop.Count = 1
	}
	left := stack
	left.Count = stack.Count - drop.Count
	if left.Count <= 0 {
		left = protocol.ItemStack{}
	}
	setStack(view, inv, slot, left, outcome)
	outcome.tookFurnaceResult(view, slot)
	outcome.throw(drop)
}

// throwResult is mode 4 on a crafting result: the grid's result drops and
// the ingredients are spent. Q throws one craft; Ctrl+Q repeats while the
// grid yields the same item, as shiftCraftFromResult loops.
func throwResult(view View, inv *player.Inventory, button int32, outcome *Outcome) {
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
	for {
		id, result := inv.CraftMatch(cells, width)
		if result.Empty() {
			return
		}
		// A plain Q removes one item from the slot, Ctrl+Q the whole result.
		// vanilla: AbstractContainerMenu.doClick THROW, ResultSlot.remove
		counted := result
		if button == 0 {
			counted.Count = 1
		}
		outcome.Crafted = append(outcome.Crafted, counted)
		consumeGrid(view, inv, cells, id, outcome)
		outcome.throw(inv.TakeCraft(result))
		if button == 0 {
			return
		}
		_, next := inv.CraftMatch(cells, width)
		if next.Empty() || next.ItemID != result.ItemID {
			return
		}
	}
}
