package inventory

import (
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/registry"
)

// Brewing stand rules: the bottle slots take one brewing input apiece, the
// ingredient slot a reagent of some brewing recipe, the fuel slot an item
// with brewing fuel; the tick brews by the brewing recipes.
// vanilla: BrewingStandBlockEntity.canPlaceItem, BrewingStandMenu, BrewingRecipe

// isPotionBottle reports whether a bottle slot takes the stack: a recipe's
// bottle, or one of the inputs tag (which adds the glass bottle).
// vanilla: PotionIngredient.isPotionInput
func isPotionBottle(stack protocol.ItemStack) bool {
	return registry.ItemInPropertySet("minecraft:brewing_input", stack.ItemID) ||
		registry.Item(stack.ItemID).InTag(registry.ItemTagIDBrewingPotionInputs)
}

// IsBrewingFuel reports whether a stack fuels a brewing stand.
// vanilla: DataComponents.BREWING_FUEL
func IsBrewingFuel(stack protocol.ItemStack) bool { return registry.HasBrewingFuel(stack) }

// isBrewingIngredient reports whether the item is some brewing recipe's
// reagent.
// vanilla: RecipePropertySet.BREWING_REAGENTS
func isBrewingIngredient(item registry.Item) bool {
	return registry.ItemInPropertySet("minecraft:brewing_reagent", int32(item))
}

// Brewable reports whether the ingredient slot holds a reagent and one of the
// first three slots a bottle some recipe brews with it.
// vanilla: BrewingStandBlockEntity.isBrewable
func Brewable(slots []protocol.ItemStack) bool {
	if len(slots) < 4 {
		return false
	}
	ingredient := slots[3]
	if ingredient.Empty() || !isBrewingIngredient(registry.Item(ingredient.ItemID)) {
		return false
	}
	for _, bottle := range slots[:3] {
		if registry.BrewingMatch(bottle, ingredient) != nil {
			return true
		}
	}
	return false
}

// BrewingMix is the bottle brewed with ingredient: a fresh stack of one of
// the recipe's result, none of the bottle's own data kept. No matching recipe
// returns it untouched.
// vanilla: BrewingStandBlockEntity.doBrew, BrewingRecipe.assemble
func BrewingMix(ingredient, bottle protocol.ItemStack) protocol.ItemStack {
	recipe := registry.BrewingMatch(bottle, ingredient)
	if recipe == nil {
		return bottle
	}
	brewed := protocol.ItemStack{ItemID: recipe.ResultID, Count: 1}
	if recipe.ResultPotion >= 0 {
		brewed.SetPotion(recipe.ResultPotion)
	}
	return brewed
}
