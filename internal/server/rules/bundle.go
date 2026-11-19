package rules

import (
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
