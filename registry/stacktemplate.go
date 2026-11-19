package registry

import (
	"math"

	"github.com/golem-mc/golem/internal/protocol"
)

// ValidStackTemplate checks the modeled constraints applied when creating a stack.
// vanilla: ItemStackTemplate.create, ItemStack.validateStrict, ItemStack.validateComponents
func ValidStackTemplate(stack protocol.ItemStack) bool {
	if stack.Count > StackMaxStackSize(stack) || !StackComponentsValid(stack) {
		return false
	}
	bundle := stack.BundleContents()
	for _, contents := range [][]protocol.ItemStack{stack.Container, bundle, stack.ChargedProjectiles()} {
		for _, item := range contents {
			if item.Count > StackMaxStackSize(item) {
				return false
			}
		}
	}
	_, ok := templateBundleWeight(bundle)
	return ok
}

// templateBundleWeight checks Fraction's signed 32-bit numerator after each
// operation. Weights use sixty-fourths, floored for a stack size that does not divide 64.
// Being over capacity is valid; only arithmetic overflow invalidates a template.
// vanilla: BundleContents.computeContentWeight, BundleContents.getWeight
func templateBundleWeight(items []protocol.ItemStack) (int64, bool) {
	var total int64
	for _, item := range items {
		weight := int64(64 / StackMaxStackSize(item))
		if contents := item.BundleContents(); len(contents) > 0 || IsBundle(item.ItemID) {
			nested, ok := templateBundleWeight(contents)
			if !ok {
				return 0, false
			}
			weight = nested + 4 // vanilla: BUNDLE_IN_BUNDLE_WEIGHT = 1/16
		} else if bees, _ := item.BeesOf(); len(bees) > 0 {
			weight = 64
		}
		if !templateWeightFits(weight) {
			return 0, false
		}
		weight *= int64(item.Count)
		if !templateWeightFits(weight) {
			return 0, false
		}
		total += weight
		if !templateWeightFits(total) {
			return 0, false
		}
	}
	return total, true
}

// templateWeightFits checks the reduced weight/64 against Fraction's numerator bound.
func templateWeightFits(weight int64) bool {
	for denominator := 64; denominator > 1 && weight%2 == 0; denominator /= 2 {
		weight /= 2
	}
	return weight <= math.MaxInt32
}
