package rules

import "github.com/golem-mc/golem/internal/protocol"

// DestroyedContents is what a destroyed item entity's stack spills: a block
// item's container, a bundle's bundle_contents, nothing from any other item.
// vanilla: BlockItem.onDestroyed, BundleItem.onDestroyed, Item.onDestroyed
func DestroyedContents(stack protocol.ItemStack, isBlockItem bool) []protocol.ItemStack {
	switch {
	case IsBundle(stack.ItemID):
		return stack.BundleContents()
	case isBlockItem:
		return stack.Container
	}
	return nil
}
