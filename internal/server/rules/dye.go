package rules

import (
	"math/rand"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/registry"
)

// The dye items and the colour mixing of wool: which items vanilla's
// DyeItem class answers for, and the colour two wool colours mix to when
// sheep breed.

// DyeItemColour is a dye item's colour word (its DYE component, own or its
// item's, the word a sheep's colour is kept by); false for an item outside the
// dyes tag, whatever component it carries.
// vanilla: DyeItem, DataComponents.DYE
func DyeItemColour(stack protocol.ItemStack) (string, bool) {
	if !registry.Item(stack.ItemID).InTag(registry.ItemTagIDDyes) {
		return "", false
	}
	c, ok := registry.StackDyeColour(stack)
	if !ok {
		return "", false
	}
	return registry.DyeNames[c], true
}

// MixedWoolColour is the colour the two dyes craft into in a 2x1 grid
// (CraftResult over the recipe tables), or a random parent on rng where no
// recipe mixes them. Sheep breeding is the only caller, so a colour without a
// dye item falls to the dice like any unmixable pair.
// vanilla: DyeColor.getMixedColor, DyeColor.findColorMixInRecipes
func MixedWoolColour(first, second string, rng *rand.Rand) string {
	a, okA := dyeItemID(first)
	b, okB := dyeItemID(second)
	if okA && okB {
		cells := []protocol.ItemStack{{ItemID: a, Count: 1}, {ItemID: b, Count: 1}}
		if colour, ok := DyeItemColour(registry.CraftResult(cells, 2)); ok {
			return colour
		}
	}
	if rng.Intn(2) == 1 {
		return first
	}
	return second
}

// dyeItemID gives the numeric ID of the dye item of a colour word, for
// the recipe grid MixedWoolColour builds its pair in.
func dyeItemID(colour string) (int32, bool) {
	return registry.ItemID(registry.Qualify(colour + "_dye"))
}
