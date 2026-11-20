package datapack

import (
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/registry"
)

// The crafting_transmute, crafting_dye and crafting_imbue recipe types, which
// rewrite one grid stack, and their recipe book displays.

// specialBase reads what the three types share: the book category, the group
// and the notification flag, as the display of the recipe.
// vanilla: CraftingRecipe.CraftingBookInfo, Recipe.CommonInfo
func specialBase(id string, o obj) (registry.CraftingDisplay, string) {
	category := bookCategory(o, "misc")
	categoryID, ok := craftingCategories[category]
	if !ok {
		fail("unknown crafting category %q", category)
	}
	return registry.CraftingDisplay{Name: id, Category: categoryID, Notify: flag(o, "show_notification", true)}, bookGroup(o)
}

// materialCount reads a transmute recipe's material_count, an exact number or
// an object of min and max, within 1 to 8.
// vanilla: TransmuteRecipe.MATERIAL_COUNT_BOUNDS
func materialCount(o obj) (lo, hi int32) {
	v, ok := o["material_count"]
	if !ok {
		return 1, 1
	}
	if f, isNum := num(v); isNum {
		lo, hi = int32(int64(f)), int32(int64(f))
	} else {
		r := object(v, "material_count")
		// A missing end is unbounded, which no allowed range encloses.
		lo, hi = int32(int64(requireNum(r, "min"))), int32(int64(requireNum(r, "max")))
	}
	if lo < 1 || hi > 8 || lo > hi {
		fail("material_count must be within 1 to 8, with min at most max")
	}
	return lo, hi
}

// transmuteRecipe reads a crafting_transmute recipe.
// vanilla: TransmuteRecipe.MAP_CODEC, TransmuteRecipe.display
func transmuteRecipe(id string, o obj, into *registry.PackRecipes) registry.PackTransmute {
	display, group := specialBase(id, o)
	input, material := recipeIngredient(required(o, "input")), recipeIngredient(required(o, "material"))
	lo, hi := materialCount(o)
	item, count, patch := resultTemplate(required(o, "result"), false)
	keepResult(into, id, patch)
	add := flag(o, "add_material_count_to_result", false)
	recipe := registry.PackTransmute{
		ID: id, Input: input.Items, Material: material.Items, MinMaterial: lo, MaxMaterial: hi,
		ResultID: item, ResultCount: count, AddMaterialCount: add,
		GroupName: group,
	}
	display.ResultID, display.ResultCount = item, count
	display.Squares = []registry.BookIngredient{bookSquare(input)}
	display.Items = []registry.Ingredient{input.Items}
	for range hi {
		display.Squares = append(display.Squares, bookSquare(material))
		display.Items = append(display.Items, material.Items)
	}
	slots := []registry.BookSlot{{Kind: registry.BookSlotIngredient, Ingredient: bookSquare(input)}}
	for materials := lo; materials <= hi; materials++ {
		slots = append(slots, registry.BookSlot{Kind: registry.BookSlotIngredient, Ingredient: bookSquare(material)})
		size := count
		if add {
			size += materials
		}
		result := registry.BookSlot{Kind: registry.BookSlotStack, ItemID: item, Count: size}
		if item == 0 {
			result = registry.BookSlot{Kind: registry.BookSlotComposite}
			for _, kept := range input.Items {
				result.Children = append(result.Children, registry.BookSlot{Kind: registry.BookSlotStack, ItemID: kept, Count: size})
			}
		}
		entry := display
		entry.Slots = &registry.CraftingSlots{Squares: append([]registry.BookSlot(nil), slots...), Result: result}
		recipe.Displays = append(recipe.Displays, entry)
	}
	return recipe
}

// dyeRecipe reads a crafting_dye recipe.
// vanilla: DyeRecipe.MAP_CODEC, DyeRecipe.display
func dyeRecipe(id string, o obj, into *registry.PackRecipes) registry.PackDye {
	display, group := specialBase(id, o)
	target, dye := recipeIngredient(required(o, "target")), recipeIngredient(required(o, "dye"))
	item, count, patch := recipeResult(o)
	keepResult(into, id, patch)
	display.ResultID, display.ResultCount = item, count
	display.Squares = []registry.BookIngredient{bookSquare(target), bookSquare(dye)}
	display.Items = []registry.Ingredient{target.Items, dye.Items}
	targetSlot := registry.BookSlot{Kind: registry.BookSlotIngredient, Ingredient: bookSquare(target)}
	dyeSlot := registry.BookSlot{
		Kind: registry.BookSlotComponent, Component: protocol.DataComponentDye,
		Children: []registry.BookSlot{{Kind: registry.BookSlotIngredient, Ingredient: bookSquare(dye)}},
	}
	display.Slots = &registry.CraftingSlots{
		Squares: []registry.BookSlot{targetSlot, dyeSlot},
		Result:  registry.BookSlot{Kind: registry.BookSlotDyed, Children: []registry.BookSlot{dyeSlot, targetSlot}},
	}
	return registry.PackDye{
		ID: id, Target: target.Items, Dye: dye.Items, ResultID: item, ResultCount: count,
		Displays: []registry.CraftingDisplay{display}, GroupName: group,
	}
}

// imbueRecipe reads a crafting_imbue recipe.
// vanilla: ImbueRecipe.MAP_CODEC, ImbueRecipe.display
func imbueRecipe(id string, o obj, into *registry.PackRecipes) registry.PackImbue {
	display, group := specialBase(id, o)
	source, material := recipeIngredient(required(o, "source")), recipeIngredient(required(o, "material"))
	item, count, patch := recipeResult(o)
	keepResult(into, id, patch)
	display.ResultID, display.ResultCount = item, count
	display.Shaped, display.Width, display.Height = true, 3, 3
	slots := &registry.CraftingSlots{}
	for i := range 9 {
		ing := material
		slot := registry.BookSlot{Kind: registry.BookSlotIngredient, Ingredient: bookSquare(material)}
		if i == 4 {
			ing = source
			slot = registry.BookSlot{Kind: registry.BookSlotPotion, Children: []registry.BookSlot{
				{Kind: registry.BookSlotIngredient, Ingredient: bookSquare(source)},
			}}
		}
		display.Squares = append(display.Squares, bookSquare(ing))
		display.Items = append(display.Items, ing.Items)
		slots.Squares = append(slots.Squares, slot)
	}
	slots.Result = registry.BookSlot{Kind: registry.BookSlotPotion, Children: []registry.BookSlot{
		{Kind: registry.BookSlotStack, ItemID: item, Count: count},
	}}
	display.Slots = slots
	return registry.PackImbue{
		ID: id, Source: source.Items, Material: material.Items, ResultID: item, ResultCount: count,
		Displays: []registry.CraftingDisplay{display}, GroupName: group,
	}
}
