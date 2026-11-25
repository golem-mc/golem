package inventory

import (
	"math/big"
	"slices"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/registry"
)

// A bundle's click path: instead of trading places, a left click takes the
// stack in and a right click gives a stored stack back.
// vanilla: BundleItem.overrideStackedOnOther, overrideOtherStackedOnMe

// isBundle reports whether the stack is one of the bundles.
func isBundle(stack protocol.ItemStack) bool {
	return rules.IsBundle(stack.ItemID)
}

// bundleMaxAdd is how many items of the given weight fit in a bundle already
// holding weight. vanilla: BundleContents.Mutable.getMaxAmountToAdd
func bundleMaxAdd(weight, itemWeight *big.Rat) int32 {
	left := new(big.Rat).Sub(big.NewRat(1, 1), weight)
	if itemWeight.Sign() <= 0 || left.Sign() <= 0 {
		return 0
	}
	q := left.Quo(left, itemWeight)
	return int32(new(big.Int).Quo(q.Num(), q.Denom()).Int64())
}

// bundleInsert inserts what fits, merging onto a same-stack entry when the
// item stacks and otherwise prepending a new entry. It returns the items and
// the count inserted.
// vanilla: BundleContents.Mutable.tryInsert
func bundleInsert(items []protocol.ItemStack, stack protocol.ItemStack) ([]protocol.ItemStack, int32) {
	itemWeight, ok := rules.BundleItemWeight(stack)
	if !ok || !rules.BundleAccepts(stack) {
		return items, 0
	}
	amount := min(stack.Count, bundleMaxAdd(rules.BundleItemsWeight(items), itemWeight))
	if amount <= 0 {
		return items, 0
	}
	if registry.StackMaxStackSize(stack) > 1 {
		for i, in := range items {
			if !in.Same(stack) {
				continue
			}
			merged := in
			merged.Count += amount
			rest := make([]protocol.ItemStack, 0, len(items))
			rest = append(rest, merged)
			rest = append(rest, items[:i]...)
			rest = append(rest, items[i+1:]...)
			return rest, amount
		}
	}
	entry := stackCount(stack, amount)
	return append([]protocol.ItemStack{entry}, items...), amount
}

// bundleStackedOnOther handles a held bundle: a left click on a stack takes
// what fits, a right click on an empty slot puts a stored stack there. It
// reports whether the bundle handled the click.
// vanilla: BundleItem.overrideStackedOnOther
func bundleStackedOnOther(s *Session, view View, inv *player.Inventory, button, slot int32, creative bool, xpLevel int32, outcome *Outcome) bool {
	if !isBundle(s.Carried) || s.Carried.Removes(protocol.DataComponentBundleContents) {
		return false
	}
	other := view.Stack(inv, slot)
	switch button {
	case 0:
		if other.Empty() {
			return false
		}
		// vanilla: Slot.tryRemove mayPickup
		if !mayPickup(view, inv, slot, creative, xpLevel) {
			outcome.BundleSounds = append(outcome.BundleSounds, registry.SoundItemBundleInsertFail)
			return true
		}
		itemWeight, ok := rules.BundleItemWeight(other)
		// A bundle refusing the stack, or full, leaves both in place rather
		// than trading; the click is still the bundle's.
		if !ok || !rules.BundleAccepts(other) {
			outcome.BundleSounds = append(outcome.BundleSounds, registry.SoundItemBundleInsertFail)
			return true
		}
		amount := min(other.Count, bundleMaxAdd(rules.BundleItemsWeight(s.Carried.BundleContents()), itemWeight))
		// vanilla: Slot.tryRemove refuses a partial take without allowModification.
		if amount < other.Count && !view.placeable(slot, other) {
			amount = 0
		}
		items, inserted := bundleInsert(s.Carried.BundleContents(), stackCount(other, amount))
		if inserted <= 0 {
			outcome.BundleSounds = append(outcome.BundleSounds, registry.SoundItemBundleInsertFail)
			return true
		}
		outcome.BundleSounds = append(outcome.BundleSounds, registry.SoundItemBundleInsert)
		s.Carried.SetBundleContents(items)
		setStack(view, inv, slot, stackCount(other, other.Count-inserted), outcome)
		outcome.tookFurnaceResult(view, slot)
		return true
	case 1:
		if !other.Empty() {
			return false
		}
		items := s.Carried.BundleContents()
		if len(items) == 0 {
			return true
		}
		// A bundle on the cursor never carries a selection: the packet only
		// names a window slot.
		item := items[0]
		rest := slices.Delete(slices.Clone(items), 0, 1)
		s.Carried.SetBundleContents(rest)
		// What the slot will not hold goes back into the bundle, the
		// remainder safeInsert hands back.
		if left := insertIntoSlot(view, inv, slot, item, outcome); !left.Empty() {
			rest, _ = bundleInsert(rest, left)
			s.Carried.SetBundleContents(rest)
		} else {
			outcome.BundleSounds = append(outcome.BundleSounds, registry.SoundItemBundleRemoveOne)
		}
		return true
	}
	return false
}

// bundleOtherStackedOnMe handles a clicked bundle: a left click with a
// held stack puts it in, a right click with an empty cursor takes a stored
// stack out. It reports whether the bundle handled the click.
// vanilla: BundleItem.overrideOtherStackedOnMe
func bundleOtherStackedOnMe(s *Session, view View, inv *player.Inventory, button, slot int32, creative bool, xpLevel int32, outcome *Outcome) bool {
	bundle := view.Stack(inv, slot)
	if !isBundle(bundle) || bundle.Removes(protocol.DataComponentBundleContents) {
		return false
	}
	switch button {
	case 0:
		if s.Carried.Empty() {
			// Left-clicking a bundle with an empty cursor picks it up like
			// any other stack, dropping its own scroll position with it.
			s.clearBundleSelectionAt(slot)
			return false
		}
		// vanilla: Slot.allowModification
		if !mayPickup(view, inv, slot, creative, xpLevel) || !view.placeable(slot, bundle) {
			outcome.BundleSounds = append(outcome.BundleSounds, registry.SoundItemBundleInsertFail)
			return true
		}
		items, inserted := bundleInsert(bundle.BundleContents(), s.Carried)
		if inserted <= 0 {
			outcome.BundleSounds = append(outcome.BundleSounds, registry.SoundItemBundleInsertFail)
			return true
		}
		outcome.BundleSounds = append(outcome.BundleSounds, registry.SoundItemBundleInsert)
		bundle.SetBundleContents(items)
		setStack(view, inv, slot, bundle, outcome)
		s.Carried.Count -= inserted
		if s.Carried.Count <= 0 {
			s.Carried = protocol.ItemStack{}
		}
		return true
	case 1:
		if !s.Carried.Empty() {
			s.clearBundleSelectionAt(slot)
			return false
		}
		if !mayPickup(view, inv, slot, creative, xpLevel) || !view.placeable(slot, bundle) {
			return true
		}
		items := bundle.BundleContents()
		if len(items) == 0 {
			return true
		}
		index := s.bundleRemoveIndex(slot, items)
		item := items[index]
		rest := slices.Delete(slices.Clone(items), index, index+1)
		bundle.SetBundleContents(rest)
		setStack(view, inv, slot, bundle, outcome)
		outcome.BundleSounds = append(outcome.BundleSounds, registry.SoundItemBundleRemoveOne)
		s.Carried = item
		s.clearBundleSelectionAt(slot)
		return true
	}
	return false
}

// insertIntoSlot puts what the empty slot takes of stack, returning the
// rest: vanilla's safeInsert minus the merge, as the slot is empty.
func insertIntoSlot(view View, inv *player.Inventory, slot int32, stack protocol.ItemStack, outcome *Outcome) protocol.ItemStack {
	if stack.Empty() || !view.placeable(slot, stack) {
		return stack
	}
	n := min(stack.Count, view.slotLimit(slot, stack))
	if n <= 0 {
		return stack
	}
	setStack(view, inv, slot, stackCount(stack, n), outcome)
	return stackCount(stack, stack.Count-n)
}

// stackCount is a stack of the given count, empty once none are left.
func stackCount(stack protocol.ItemStack, count int32) protocol.ItemStack {
	if count <= 0 {
		return protocol.ItemStack{}
	}
	stack.Count = count
	return stack
}
