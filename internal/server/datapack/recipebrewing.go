package datapack

import "github.com/golem-mc/golem/registry"

// brewingRecipes expands an input potion set into the table's one-potion rows.
// The table cannot represent reagent predicates or arbitrary output components.
// vanilla: BrewingRecipe.MAP_CODEC, PotionIngredient.MAP_CODEC, PotionsPredicate.CODEC
func (decoder ResourceDecoder) brewingRecipes(id string, o obj) []registry.BrewingRecipe {
	input := object(required(o, "input"), "input")
	unknownKeys(input, "brewing input", "item", "potion_contents")
	items := decoder.recipeIngredient(required(input, "item")).Items
	reagent := object(required(o, "reagent"), "reagent")
	unknownKeys(reagent, "brewing reagent", "item")
	reagents := decoder.recipeIngredient(required(reagent, "item")).Items

	potions := []int32{-1}
	if v, ok := input["potion_contents"]; ok {
		predicate := object(v, "input.potion_contents")
		unknownKeys(predicate, "input.potion_contents", "potions")
		potions = nil
		for _, name := range holderSet(required(predicate, "potions"), "potion", decoder.tags.PotionTagMembers) {
			potions = append(potions, recipePotion(name))
		}
		if len(potions) == 0 {
			fail("an empty input.potion_contents.potions set is not supported by this server")
		}
	}

	output := object(required(o, "output"), "output")
	unknownKeys(output, "brewing output", "id", "count", "components")
	result := recipeItem(requireStr(output, "id"))
	if _, ok := output["count"]; ok && int32(int64(requireNum(output, "count"))) != 1 {
		fail("brewing output count other than 1 is not supported by this server")
	}
	resultPotion := int32(-1)
	if v, ok := output["components"]; ok {
		components := object(v, "output.components")
		unknownKeys(components, "brewing output component", "minecraft:potion_contents")
		if v, ok := components["minecraft:potion_contents"]; ok {
			contents := object(v, "output potion_contents")
			unknownKeys(contents, "brewing output potion_contents", "potion")
			resultPotion = recipePotion(requireStr(contents, "potion"))
		}
	}
	out := make([]registry.BrewingRecipe, 0, len(potions))
	for _, potion := range potions {
		out = append(out, registry.BrewingRecipe{
			ID: id, Input: items, Potion: potion, Reagent: reagents, ResultID: result, ResultPotion: resultPotion,
		})
	}
	return out
}

func recipePotion(name string) int32 {
	id, ok := registry.PotionID(registry.Qualify(name))
	if !ok {
		fail("unknown potion %s", name)
	}
	return id
}
