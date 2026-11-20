package registry

import (
	"slices"

	"github.com/golem-mc/golem/internal/protocol"
)

// The crafting_transmute, crafting_dye and crafting_imbue recipes. Their
// results carry the components of a grid stack, so they are matched here
// instead of from a fixed result.

// ingredientFits reports whether a non-empty stack is one of the ingredient's items.
func ingredientFits(ingredient Ingredient, cell protocol.ItemStack) bool {
	return !cell.Empty() && slices.Contains(ingredient, cell.ItemID)
}

// resultFrom is the stack a recipe makes of an input: the input's components
// on the result item, the count, and the pack's result patch over them.
// vanilla: TransmuteRecipe.createWithOriginalComponents, ItemStackTemplate.apply
func resultFrom(id string, input protocol.ItemStack, item, count int32) protocol.ItemStack {
	result := input
	if item != 0 {
		result.ItemID = item
	}
	result.Count = count
	result.Raw = slices.Clone(input.Raw)
	return RecipeResult(id, result)
}

// result matches a crafting_transmute recipe: one input stack and
// MinMaterial to MaxMaterial material stacks.
// vanilla: TransmuteRecipe.matches, TransmuteRecipe.assemble
func (r *TransmuteRecipe) result(cells []protocol.ItemStack, filled int) (protocol.ItemStack, bool) {
	if filled < int(r.MinMaterial)+1 || filled > int(r.MaxMaterial)+1 {
		return protocol.ItemStack{}, false
	}
	var input protocol.ItemStack
	materials := int32(0)
	for _, cell := range cells {
		switch {
		case cell.Empty():
		case ingredientFits(r.Input, cell):
			if !input.Empty() {
				return protocol.ItemStack{}, false
			}
			input = cell
		case ingredientFits(r.Material, cell):
			if materials++; materials > r.MaxMaterial {
				return protocol.ItemStack{}, false
			}
		default:
			return protocol.ItemStack{}, false
		}
	}
	if input.Empty() || materials < r.MinMaterial {
		return protocol.ItemStack{}, false
	}
	count := r.ResultCount
	if r.AddMaterialCount {
		count += materials
	}
	result := resultFrom(r.ID, input, r.ResultID, count)
	// A single result that equals its input changes nothing, so it is no recipe.
	if count == 1 && (result.Empty() || input.Same(result)) {
		return protocol.ItemStack{}, false
	}
	return result, true
}

// result matches a crafting_dye recipe: one target and at least one dye, the
// result the target recoloured.
// vanilla: DyeRecipe.matches, DyeRecipe.assemble
func (r *DyeRecipe) result(cells []protocol.ItemStack, filled int) (protocol.ItemStack, bool) {
	if filled < 2 {
		return protocol.ItemStack{}, false
	}
	var target protocol.ItemStack
	var dyes []int
	for _, cell := range cells {
		if cell.Empty() {
			continue
		}
		if ingredientFits(r.Target, cell) {
			if !target.Empty() {
				return protocol.ItemStack{}, false
			}
			target = cell
			continue
		}
		colour, ok := StackDyeColour(cell)
		if !ok || !ingredientFits(r.Dye, cell) {
			return protocol.ItemStack{}, false
		}
		dyes = append(dyes, int(colour))
	}
	if target.Empty() || len(dyes) == 0 {
		return protocol.ItemStack{}, false
	}
	current, has := target.DyedColorOf()
	result := resultFrom(r.ID, target, r.ResultID, r.ResultCount)
	result.SetDyedColor(mixDyes(current, has, dyes))
	return result, true
}

// result matches a crafting_imbue recipe: a 3x3 grid of the material around
// the source, the result taking the source's potion_contents.
// vanilla: ImbueRecipe.matches, ImbueRecipe.assemble
func (r *ImbueRecipe) result(cells []protocol.ItemStack, filled int) (protocol.ItemStack, bool) {
	if len(cells) != 9 || filled != 9 || !ingredientFits(r.Source, cells[4]) {
		return protocol.ItemStack{}, false
	}
	for i, cell := range cells {
		if i != 4 && !ingredientFits(r.Material, cell) {
			return protocol.ItemStack{}, false
		}
	}
	result := RecipeResult(r.ID, protocol.ItemStack{ItemID: r.ResultID, Count: r.ResultCount})
	if contents, ok := cells[4].PotionContentsOf(); ok {
		result.SetPotionContents(contents)
	}
	return result, true
}
