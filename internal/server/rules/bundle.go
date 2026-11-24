package rules

import (
	"math/big"
	"slices"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/registry"
)

// BundleUseTicks is how long a bundle use lasts. vanilla: BundleItem.getUseDuration
const BundleUseTicks = 200

// IsBundle reports whether the item is one of the bundles.
func IsBundle(item int32) bool {
	return registry.IsBundle(item)
}

// BundleDropsOnUseTick reports whether the use tick that has run tick ticks
// before it (0 for the first) empties a stack: the first tick, then every
// other tick once ten have passed.
// vanilla: BundleItem.onUseTick
func BundleDropsOnUseTick(tick int) bool {
	remaining := BundleUseTicks - tick
	return tick == 0 || (remaining < BundleUseTicks-10 && remaining%2 == 0)
}

// BundleRemoveFirst takes the first (newest) stack out of a bundle and
// returns the bundle without it. ok is false for an empty bundle.
// vanilla: BundleContents.Mutable.removeOne
func BundleRemoveFirst(bundle protocol.ItemStack) (rest, removed protocol.ItemStack, ok bool) {
	items := bundle.BundleContents()
	if len(items) == 0 {
		return bundle, protocol.ItemStack{}, false
	}
	rest = bundle
	rest.SetBundleContents(slices.Delete(slices.Clone(items), 0, 1))
	return rest, items[0], true
}

// BundleItemWeight returns an item's weight inside a bundle and whether a
// bundle accepts it: 1/maxStackSize, a nested bundle 1/16 plus its own weight,
// a stack with bees inside a whole bundle, and shulker boxes not at all.
// vanilla: BundleContents.canItemBeInBundle, BundleContents.getWeight
func BundleItemWeight(stack protocol.ItemStack) (*big.Rat, bool) {
	if stack.Empty() || registry.Item(stack.ItemID).InTag(registry.ItemTagIDShulkerBoxes) {
		return nil, false
	}
	if IsBundle(stack.ItemID) && !stack.Removes(protocol.DataComponentBundleContents) {
		inner := BundleItemsWeight(stack.BundleContents())
		return inner.Add(inner, big.NewRat(1, 16)), true
	}
	if bees, ok := stack.BeesOf(); ok && len(bees) > 0 {
		return big.NewRat(1, 1), true
	}
	return big.NewRat(1, int64(registry.StackMaxStackSize(stack))), true
}

// BundleItemsWeight returns the total weight of a bundle's stored stacks.
// vanilla: BundleContents.computeContentWeight
func BundleItemsWeight(items []protocol.ItemStack) *big.Rat {
	total := new(big.Rat)
	for _, item := range items {
		if weight, ok := BundleItemWeight(item); ok {
			total.Add(total, weight.Mul(weight, big.NewRat(int64(item.Count), 1)))
		}
	}
	return total
}
