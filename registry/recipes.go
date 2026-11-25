package registry

import (
	"slices"

	"github.com/golem-mc/golem/internal/protocol"
)

// Crafting grid matching against the generated tables (recipes_gen.go). It
// lives here rather than in server because the player window's 2x2 grid is
// four inventory slots, so its worth is part of the inventory; taking the
// result is in internal/server/crafting.go.

// CraftResult returns the stack the result slot shows for a grid, empty
// when no recipe matches. width is the cells per row: 2 or 3.
func CraftResult(cells []protocol.ItemStack, width int32) protocol.ItemStack {
	_, result := CraftMatch(cells, width)
	return result
}

// CraftMatch returns the ID of the recipe a grid crafts and its result, both
// empty when none matches. The ID is empty for a special recipe other than
// the decorated pot.
func CraftMatch(cells []protocol.ItemStack, width int32) (string, protocol.ItemStack) {
	filled := 0
	for i := range cells {
		if !cells[i].Empty() {
			filled++
		}
	}
	// No recipe, stew or special one matches an empty grid.
	// vanilla: RecipeMap.getRecipesFor
	if filled == 0 {
		return "", protocol.ItemStack{}
	}
	grid := gridBoxOf(cells, width)
	set := recipes()
	for _, ref := range set.craftOrder {
		if ref.cells != 0 && int(ref.cells) != filled {
			continue
		}
		switch ref.kind {
		case craftShaped:
			if r := &set.shaped[ref.index]; shapedIn(r, ref.symmetric, cells, width, grid) {
				return r.ID, RecipeResult(r.ID, protocol.ItemStack{ItemID: r.ResultID, Count: r.ResultCount})
			}
		case craftShapeless:
			if r := &set.shapeless[ref.index]; shapelessFits(r, cells) {
				return r.ID, RecipeResult(r.ID, protocol.ItemStack{ItemID: r.ResultID, Count: r.ResultCount})
			}
		case craftTransmute:
			r := &set.transmute[ref.index]
			if result, ok := r.result(cells, filled); ok {
				return r.ID, result
			}
		case craftDye:
			r := &set.dye[ref.index]
			if result, ok := r.result(cells, filled); ok {
				return r.ID, result
			}
		case craftImbue:
			r := &set.imbue[ref.index]
			if result, ok := r.result(cells, filled); ok {
				return r.ID, result
			}
		}
	}
	if id, result, ok := stewResult(cells); ok {
		return id, result
	}
	result := specialCraftResult(cells)
	if result.ItemID == potItem && !result.Empty() {
		return "minecraft:decorated_pot", result
	}
	return "", result
}

// CraftingDisplayOf returns the crafting recipe a recipe book display ID
// names, or nil: IDs number every recipe kind's displays, so a cooking or
// smithing ID reads as none. vanilla: RecipeManager.getRecipeFromDisplay
func CraftingDisplayOf(id int32) *CraftingDisplay {
	set := recipes()
	if i, ok := set.displayByID[id]; ok {
		return &set.displays[i]
	}
	return nil
}

// CraftingDisplayOfName returns the crafting recipe a player file's
// recipeBook lists under the name, or nil when no such recipe has a display.
func CraftingDisplayOfName(name string) *CraftingDisplay {
	set := recipes()
	if i, ok := set.displayByName[name]; ok {
		return &set.displays[i]
	}
	return nil
}

// FurnaceDisplayOf returns the cooking recipe a recipe book display ID names,
// or nil.
func FurnaceDisplayOf(id int32) *FurnaceDisplay {
	set := recipes()
	if i, ok := set.furnaceByID[id]; ok {
		return &set.furnaces[i]
	}
	return nil
}

// FurnaceDisplayOfName returns the cooking recipe a player file's recipeBook
// lists under the name, or nil.
func FurnaceDisplayOfName(name string) *FurnaceDisplay {
	set := recipes()
	if i, ok := set.furnaceByName[name]; ok {
		return &set.furnaces[i]
	}
	return nil
}

// StonecutterDisplayOfName returns a stonecutting recipe's display, or nil.
func StonecutterDisplayOfName(name string) *StonecutterDisplay {
	set := recipes()
	if i, ok := set.stonecutterByName[name]; ok {
		return &set.stonecutters[i]
	}
	return nil
}

// SmithingDisplayOfName returns a smithing recipe's display, or nil.
func SmithingDisplayOfName(name string) *SmithingDisplay {
	set := recipes()
	if i, ok := set.smithingByName[name]; ok {
		return &set.smithingDisplays[i]
	}
	return nil
}

// RecipeDisplayIDOfName returns the display ID of a recipe
// by name, false for a name no display carries.
func RecipeDisplayIDOfName(name string) (int32, bool) {
	if d := CraftingDisplayOfName(name); d != nil {
		return d.ID, true
	}
	if d := FurnaceDisplayOfName(name); d != nil {
		return d.ID, true
	}
	if d := StonecutterDisplayOfName(name); d != nil {
		return d.ID, true
	}
	if d := SmithingDisplayOfName(name); d != nil {
		return d.ID, true
	}
	return 0, false
}

// ShapedPatternFits reports whether a shrunk shaped recipe lies in the grid unrotated, mirrored or not, with every cell
// outside it empty. pattern is
// Width*Height row-major option lists, nil for a square left out.
func ShapedPatternFits(width, height int32, pattern []Ingredient, cells []protocol.ItemStack, gridWidth int32) bool {
	return shapedFits(&ShapedRecipe{Width: width, Height: height, Pattern: pattern}, cells, gridWidth)
}

// ShapelessIngredientsFit reports whether the grid holds exactly the
// shapeless ingredients, one option list each, in any arrangement.
func ShapelessIngredientsFit(ingredients []Ingredient, cells []protocol.ItemStack) bool {
	return shapelessFits(&ShapelessRecipe{Ingredients: ingredients}, cells)
}

// gridBox is the bounding box of a grid's non-empty cells; w and h are 0
// when there are none.
type gridBox struct{ x, y, w, h int8 }

// gridBoxOf returns the box of a width-wide grid's non-empty cells.
func gridBoxOf(cells []protocol.ItemStack, width int32) gridBox {
	height := int32(len(cells)) / width
	minX, minY, maxX, maxY := width, height, int32(-1), int32(-1)
	for y := range height {
		for x := range width {
			if !cells[y*width+x].Empty() {
				minX, minY, maxX, maxY = min(minX, x), min(minY, y), max(maxX, x), max(maxY, y)
			}
		}
	}
	if maxX < 0 {
		return gridBox{}
	}
	return gridBox{int8(minX), int8(minY), int8(maxX - minX + 1), int8(maxY - minY + 1)}
}

// shapedFits reports whether the pattern covers the grid's filled cells
// exactly, mirrored or not.
func shapedFits(recipe *ShapedRecipe, cells []protocol.ItemStack, width int32) bool {
	return shapedIn(recipe, symmetric(recipe), cells, width, gridBoxOf(cells, width))
}

// symmetric reports whether the pattern reads the same mirrored, so the
// mirrored placement need not be tried.
// vanilla: ShapedRecipePattern.symmetrical
func symmetric(recipe *ShapedRecipe) bool {
	for y := range recipe.Height {
		for x := range recipe.Width / 2 {
			row := recipe.Pattern[y*recipe.Width : (y+1)*recipe.Width]
			if !slices.Equal(row[x], row[recipe.Width-1-x]) {
				return false
			}
		}
	}
	return true
}

// shapedIn is shapedFits given the grid's box. Every pattern arrives
// shrunk, a symbol on each edge, so the grid's box must be Width x Height
// and the only placement is the pattern laid on it.
// vanilla: ShapedRecipePattern.matches, ShapedRecipePattern.shrink
func shapedIn(recipe *ShapedRecipe, symmetric bool, cells []protocol.ItemStack, width int32, grid gridBox) bool {
	if int32(grid.w) != recipe.Width || int32(grid.h) != recipe.Height {
		return false
	}
	column, row := int32(grid.x), int32(grid.y)
	return shapedAt(recipe, cells, width, column, row, false) || !symmetric && shapedAt(recipe, cells, width, column, row, true)
}

// shapedAt tries one placement at (column, row), mirrored by reading column
// recipe.Width-1-x. The whole grid is walked: cells under the pattern match
// it (an empty ingredient on an empty cell) and the rest must be empty, as
// vanilla tests them against the empty ingredient.
func shapedAt(recipe *ShapedRecipe, cells []protocol.ItemStack, width, column, row int32, mirror bool) bool {
	height := int32(len(cells)) / width
	for y := range height {
		for x := range width {
			patternX, patternY := x-column, y-row
			if patternX < 0 || patternX >= recipe.Width || patternY < 0 || patternY >= recipe.Height {
				// Outside the placement, where the recipe wants nothing.
				if !cells[y*width+x].Empty() {
					return false
				}
				continue
			}
			if mirror {
				patternX = recipe.Width - 1 - patternX
			}
			ingredient := recipe.Pattern[patternY*recipe.Width+patternX]
			cell := &cells[y*width+x]
			if len(ingredient) == 0 {
				if !cell.Empty() {
					return false
				}
				continue
			}
			if cell.Empty() || !slices.Contains(ingredient, cell.ItemID) {
				return false
			}
		}
	}
	return true
}

// shapelessFits pairs each ingredient with exactly one non-empty cell and
// every non-empty cell with one ingredient. An ingredient may match several
// cells (any plank), so pairing uses augmenting paths, not first come.
// A grid has at most 9 cells, so the pairing state lives in fixed arrays.
func shapelessFits(recipe *ShapelessRecipe, cells []protocol.ItemStack) bool {
	var filled [9]int
	n := 0
	for i := range cells {
		if cells[i].Empty() {
			continue
		}
		if n == len(filled) {
			return false
		}
		filled[n] = i
		n++
	}
	if n != len(recipe.Ingredients) {
		return false
	}
	// matched[ingredient] is the cell it is paired with, or -1.
	var matched [9]int
	for i := range matched {
		matched[i] = -1
	}
	for _, cell := range filled[:n] {
		var tried [9]bool
		if !pairCell(recipe, cells, cell, &matched, &tried) {
			return false
		}
	}
	return true
}

// pairCell pairs one cell with an ingredient, moving already-paired cells
// to other ingredients where needed: without that, coal and an oak plank
// would fail [plank, coal] by giving coal's cell the plank ingredient.
func pairCell(recipe *ShapelessRecipe, cells []protocol.ItemStack, cell int, matched *[9]int, tried *[9]bool) bool {
	for i, ingredient := range recipe.Ingredients {
		if tried[i] || !slices.Contains(ingredient, cells[cell].ItemID) {
			continue
		}
		tried[i] = true
		if matched[i] < 0 || pairCell(recipe, cells, matched[i], matched, tried) {
			matched[i] = cell
			return true
		}
	}
	return false
}

// CookingMatch returns the recipe a stack cooks under for a block entity
// kind (smelting, blasting, smoking or campfire), or nil; a kind cooking
// none of the four falls back to smelting.
// vanilla: RecipeManager.getRecipeFor
func CookingMatch(kind string, stack protocol.ItemStack) *CookingRecipe {
	return CookingMatchCached(kind, stack, nil)
}

// CookingMatchCached is CookingMatch for a block entity's own check: the
// recipe that last matched, kept in *last, wins over an earlier match while
// it still takes the stack.
// vanilla: RecipeManager.createCheck, RecipeManager.getRecipeFor with a hint
func CookingMatchCached(kind string, stack protocol.ItemStack, last *string) *CookingRecipe {
	if stack.Empty() {
		return nil
	}
	table := cookingTable(kind)
	if last != nil && *last != "" {
		for i := range table {
			if table[i].ID == *last && slices.Contains(table[i].Ingredient, stack.ItemID) {
				return &table[i]
			}
		}
	}
	for i := range table {
		if slices.Contains(table[i].Ingredient, stack.ItemID) {
			if last != nil {
				*last = table[i].ID
			}
			return &table[i]
		}
	}
	return nil
}

// CookingRecipeByID returns a cooking kind's recipe by the id a RecipesUsed
// compound keys it by, or false.
func CookingRecipeByID(kind, id string) (*CookingRecipe, bool) {
	table := cookingTable(kind)
	for i := range table {
		if table[i].ID == id {
			return &table[i], true
		}
	}
	return nil, false
}

// cookingTable is the recipe table a block entity kind cooks from; anything
// not a blast furnace, smoker or campfire uses smelting.
func cookingTable(kind string) []CookingRecipe {
	set := recipes()
	switch kind {
	case "minecraft:blast_furnace":
		return set.cooking[CookingBlasting]
	case "minecraft:smoker":
		return set.cooking[CookingSmoking]
	case "minecraft:campfire":
		return set.cooking[CookingCampfire]
	}
	return set.cooking[CookingSmelting]
}

// SmeltingResult returns what a stack smelts into, or nothing.
func SmeltingResult(stack protocol.ItemStack) protocol.ItemStack {
	recipe := SmeltingMatch(stack)
	if recipe == nil {
		return protocol.ItemStack{}
	}
	return CookingResult(recipe)
}

// SmeltingMatch returns the smelting recipe a stack smelts under, or nil
// when there is none.
func SmeltingMatch(stack protocol.ItemStack) *CookingRecipe {
	return CookingMatch("minecraft:furnace", stack)
}

// SmeltingRecipeByID returns the smelting recipe by the id a furnace's
// RecipesUsed keys it by, or false.
func SmeltingRecipeByID(id string) (*CookingRecipe, bool) {
	return CookingRecipeByID("minecraft:furnace", id)
}

// BrewingMatch returns the recipe that brews the bottle with the reagent, or
// nil. A recipe with Potion -1 accepts its input item with any or no potion.
// vanilla: BrewingRecipe.matches, PotionsPredicate.matches
func BrewingMatch(bottle, reagent protocol.ItemStack) *BrewingRecipe {
	if bottle.Empty() || reagent.Empty() {
		return nil
	}
	contents, ok := bottle.PotionContentsOf()
	if !ok {
		// Distinct from -1, which is the recipe's wildcard.
		contents.Potion = -2
	}
	table := recipes().brewing
	for i := range table {
		r := &table[i]
		if (r.Potion == contents.Potion || r.Potion == -1) && slices.Contains(r.Input, bottle.ItemID) && slices.Contains(r.Reagent, reagent.ItemID) {
			return r
		}
	}
	return nil
}

// ItemInPropertySet reports whether a recipe property set (the recipe book
// filters, which smithing slots accept by) holds the item.
func ItemInPropertySet(set string, item int32) bool {
	for _, s := range recipes().propertySets {
		if s.Name == set {
			return slices.Contains(s.Items, item)
		}
	}
	return false
}

// SmithingResult returns the smithing result slot's stack, nothing when no
// recipe takes the inputs or the trim is one the base already wears.
// vanilla: SmithingMenu.createResult, SmithingTransformRecipe.assemble,
// SmithingTrimRecipe.assemble
func SmithingResult(template, base, addition protocol.ItemStack) protocol.ItemStack {
	_, result := SmithingMatch(template, base, addition)
	return result
}

// SmithingMatch returns the ID of the smithing recipe that takes the three
// inputs and its result; the ID is set even when the result is empty because
// the trim is one the base already wears, and empty when no recipe takes them.
func SmithingMatch(template, base, addition protocol.ItemStack) (string, protocol.ItemStack) {
	set := recipes()
	for i := range set.smithing {
		recipe := &set.smithing[i]
		if !smithingFits(recipe.Template, template) ||
			!smithingFits(recipe.Base, base) || !smithingFits(recipe.Addition, addition) {
			continue
		}
		return recipe.ID, resultFrom(recipe.ID, base, recipe.ResultID, recipe.ResultCount)
	}
	for i := range set.trim {
		recipe := &set.trim[i]
		if !smithingFits(recipe.Template, template) ||
			!smithingFits(recipe.Base, base) || !smithingFits(recipe.Addition, addition) {
			continue
		}
		material, ok := StackTrimMaterial(addition)
		if !ok {
			// The recipe took the inputs but the addition provides no trim
			// material, so there is nothing its make can lay on the base.
			return recipe.ID, protocol.ItemStack{}
		}
		trimmed := base
		trimmed.Count = 1
		// The result sits in the window's menu beside the base it was
		// built from, whose raw slice is not the assembling's to rewrite.
		trimmed.Raw = slices.Clone(base.Raw)
		if m, pattern, ok := trimmed.TrimOf(); ok && m == material && pattern == recipe.Pattern {
			// The trim the base already wears, made again.
			return recipe.ID, protocol.ItemStack{}
		}
		trimmed.SetTrim(material, recipe.Pattern)
		return recipe.ID, trimmed
	}
	return "", protocol.ItemStack{}
}

// smithingFits reports whether an ingredient takes the stack: an omitted
// (nil) one takes only an empty slot, a tag that expanded empty nothing.
// vanilla: SmithingRecipe.matches, Ingredient.testOptionalIngredient
func smithingFits(ingredient Ingredient, stack protocol.ItemStack) bool {
	if ingredient == nil {
		return stack.Empty()
	}
	return slices.Contains(ingredient, stack.ItemID)
}

// StonecutterDisplayOf returns the stonecutting display of id, or nil.
func StonecutterDisplayOf(id int32) *StonecutterDisplay {
	set := recipes()
	if i, ok := set.stonecutterByID[id]; ok {
		return &set.stonecutters[i]
	}
	return nil
}

// SmithingDisplayOf returns the smithing display of id, or nil.
func SmithingDisplayOf(id int32) *SmithingDisplay {
	set := recipes()
	if i, ok := set.smithingByID[id]; ok {
		return &set.smithingDisplays[i]
	}
	return nil
}
