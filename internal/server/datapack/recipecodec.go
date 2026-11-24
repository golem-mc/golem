package datapack

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// The recipe codec reads data pack recipe files into the registry shapes the
// crafting, cooking, smithing, brewing and stonecutter code matches. It reads the
// types and fields the built-in tables hold and refuses the rest, so a recipe
// works as written or is not loaded.
// vanilla: RecipeManager.prepare, Recipe.CODEC

// cookingTypes are the cooking recipe types, in registry.Cooking* order, with
// the recipe_book_category ID of each "category" a recipe may name (the IDs
// are RecipeBookCategories' order).
// vanilla: SmeltingRecipe, BlastingRecipe, SmokingRecipe and
// CampfireCookingRecipe MAP_CODEC and recipeBookCategory
var cookingTypes = [...]struct {
	name       string
	categories map[string]int32
}{
	registry.CookingSmelting: {"minecraft:smelting", map[string]int32{"food": 4, "blocks": 5, "misc": 6}},
	registry.CookingBlasting: {"minecraft:blasting", map[string]int32{"food": 8, "blocks": 7, "misc": 8}},
	registry.CookingSmoking:  {"minecraft:smoking", map[string]int32{"food": 9, "blocks": 9, "misc": 9}},
	registry.CookingCampfire: {"minecraft:campfire_cooking", map[string]int32{"food": 12, "blocks": 12, "misc": 12}},
}

// craftingCategories are the crafting book categories a recipe's "category"
// names, by the ID the crafting displays carry.
// vanilla: CraftingBookCategory
var craftingCategories = map[string]int32{"building": 0, "redstone": 1, "equipment": 2, "misc": 3}

// builtInRecipeTypes are the types whose behaviour is built into this server
// (or computed by the client) rather than read from a file.
var builtInRecipeTypes = []string{
	"minecraft:crafting_decorated_pot", "minecraft:crafting_special_",
}

// ErrBuiltInRecipeType is the error of a recipe file whose type the server
// computes itself: the recipe is valid, only not read from the file.
var ErrBuiltInRecipeType = errors.New("recipe type is built in and not read from data packs")

// DecodeRecipe reads one recipe file under the resource id into the set. A
// file that does not decode adds nothing.
// vanilla: RecipeManager.prepare
func DecodeRecipe(id string, data []byte, into *registry.PackRecipes) (err error) {
	return liveResourceDecoder().DecodeRecipe(id, data, into)
}

func (decoder ResourceDecoder) DecodeRecipe(id string, data []byte, into *registry.PackRecipes) (err error) {
	defer recoverLoot(&err)
	o := object(parseJSON(data), "recipe")
	kind := registry.Qualify(requireStr(o, "type"))
	switch kind {
	case "minecraft:crafting_shaped":
		into.Crafting = append(into.Crafting, decoder.shapedRecipe(id, o, into))
	case "minecraft:crafting_shapeless":
		into.Crafting = append(into.Crafting, decoder.shapelessRecipe(id, o, into))
	case "minecraft:crafting_transmute":
		into.Transmute = append(into.Transmute, decoder.transmuteRecipe(id, o, into))
	case "minecraft:crafting_dye":
		into.Dye = append(into.Dye, decoder.dyeRecipe(id, o, into))
	case "minecraft:crafting_imbue":
		into.Imbue = append(into.Imbue, decoder.imbueRecipe(id, o, into))
	case "minecraft:brewing":
		into.Brewing = append(into.Brewing, decoder.brewingRecipes(id, o)...)
	case "minecraft:stonecutting":
		result, count, patch := decoder.recipeResult(o)
		keepResult(into, id, patch)
		for _, item := range decoder.recipeIngredient(required(o, "ingredient")).Items {
			into.Stonecutting = append(into.Stonecutting, registry.StonecutterRecipe{ID: id, Input: item, ResultID: result, ResultCount: count})
		}
	case "minecraft:smithing_transform":
		result, count, patch := decoder.recipeResult(o)
		keepResult(into, id, patch)
		into.SmithingTransform = append(into.SmithingTransform, registry.SmithingTransformRecipe{
			ID: id, Template: decoder.optionalIngredient(o, "template"), Base: decoder.recipeIngredient(required(o, "base")).Items,
			Addition: decoder.optionalIngredient(o, "addition"), ResultID: result, ResultCount: count,
		})
	case "minecraft:smithing_trim":
		into.SmithingTrim = append(into.SmithingTrim, registry.SmithingTrimRecipe{
			ID: id, Template: decoder.recipeIngredient(required(o, "template")).Items, Base: decoder.recipeIngredient(required(o, "base")).Items,
			Addition: decoder.recipeIngredient(required(o, "addition")).Items, Pattern: trimPattern(required(o, "pattern")),
		})
	default:
		for k, cooking := range cookingTypes {
			if cooking.name == kind {
				into.Cooking[k] = append(into.Cooking[k], decoder.cookingRecipe(id, o, cooking.categories, into))
				return nil
			}
		}
		for _, builtIn := range builtInRecipeTypes {
			if strings.HasPrefix(kind, builtIn) {
				return fmt.Errorf("%w: %s", ErrBuiltInRecipeType, kind)
			}
		}
		fail("unknown recipe type %s", kind)
	}
	return nil
}

// recipeItem returns the ID of a non-air item.
func recipeItem(name string) int32 {
	name = registry.Qualify(name)
	id, ok := registry.ItemID(name)
	if !ok || name == "minecraft:air" {
		fail("unknown item %s", name)
	}
	return id
}

// recipeResult reads a recipe's result, an item name or an object of id and
// count, and the component patch the object may carry. A component the
// stored-stack reader refuses refuses the recipe.
// vanilla: ItemStackTemplate.CODEC
func (decoder ResourceDecoder) recipeResult(o obj) (item, count int32, patch []registry.ResultComponent) {
	return decoder.resultTemplate(required(o, "result"), true)
}

// resultTemplate reads a result; needID false allows an object without an id,
// which makes item 0, the input's item.
// vanilla: ItemStackTemplate.CODEC, TransmuteResult.CODEC
func (decoder ResourceDecoder) resultTemplate(v any, needID bool) (item, count int32, patch []registry.ResultComponent) {
	switch r := v.(type) {
	case string:
		return recipeItem(r), 1, nil
	case map[string]any:
		if comps, ok := r["components"]; ok {
			patch = decoder.resultPatch(object(comps, "components"))
		}
		count = 1
		if _, ok := r["count"]; ok {
			// vanilla: ExtraCodecs.intRange(1, 99) over Codec.INT, which truncates
			// a fraction and wraps past int range before the range check
			count = int32(int64(requireNum(r, "count")))
			if count < 1 || count > 99 {
				fail("result count %v is not in 1..99", r["count"])
			}
		}
		if _, has := r["id"]; !has && !needID {
			return 0, count, patch
		}
		return recipeItem(requireStr(r, "id")), count, patch
	}
	fail("result is not an item or an object")
	return 0, 0, nil
}

// resultPatch reads a result's "components" in the component codecs' shapes.
// vanilla: DataComponentPatch.CODEC
func (decoder ResourceDecoder) resultPatch(comps obj) []registry.ResultComponent {
	var patch []registry.ResultComponent
	var probe protocol.ItemStack
	for _, name := range sortedKeys(comps) {
		tag, n, err := nbt.ParseValueTag(snbt(comps[name]))
		if err != nil || n == 0 {
			fail("Failed to read %s", name)
		}
		if !decoder.tags.ReadComponent(&probe, name, tag) {
			fail("Failed to decode %s: %s", name, snbt(comps[name]))
		}
		patch = append(patch, registry.ResultComponent{Name: name, Value: tag})
	}
	return patch
}

// keepResult records a recipe's result patch under its ID.
func keepResult(into *registry.PackRecipes, id string, patch []registry.ResultComponent) {
	if len(patch) == 0 {
		delete(into.Results, id)
		return
	}
	if into.Results == nil {
		into.Results = map[string][]registry.ResultComponent{}
	}
	into.Results[id] = patch
}

// ingredient is one recipe ingredient: the items it takes, and the tag the
// client resolves itself when the file named one. Listed is an item list in
// the file's order, duplicates kept, as the book display sends it.
// vanilla: Ingredient.display
type ingredient struct {
	Items  []int32
	Listed []int32
	Tag    string
}

// recipeIngredient reads an item, a list of items or a #tag, which must hold
// at least one item.
// vanilla: Ingredient.CODEC
func (decoder ResourceDecoder) recipeIngredient(v any) ingredient {
	ing := ingredient{}
	if s, ok := v.(string); ok && strings.HasPrefix(s, "#") {
		ing.Tag = registry.Qualify(s[1:])
	}
	for _, name := range holderSet(v, "item", decoder.tags.ItemTagMembers) {
		ing.Items = append(ing.Items, recipeItem(name))
	}
	if ing.Tag == "" {
		ing.Listed = slices.Clone(ing.Items)
	}
	slices.Sort(ing.Items)
	ing.Items = slices.Compact(ing.Items)
	if len(ing.Items) == 0 {
		fail("an ingredient cannot be empty")
	}
	return ing
}

// optionalIngredient is recipeIngredient for a field a smithing recipe may
// leave out, nil when it does.
func (decoder ResourceDecoder) optionalIngredient(o obj, key string) []int32 {
	v, ok := o[key]
	if !ok {
		return nil
	}
	return decoder.recipeIngredient(v).Items
}

// bookSquare is an ingredient as the recipe book's display carries it: the tag
// the client resolves, or the items.
func bookSquare(ing ingredient) registry.BookIngredient {
	if ing.Tag != "" {
		return registry.BookIngredient{Tag: ing.Tag}
	}
	return registry.BookIngredient{Items: ing.Listed}
}

// bookCategory returns the "category" a recipe's book info names, def when it
// names none.
// vanilla: Recipe.BookInfo.mapCodec
func bookCategory(o obj, def string) string {
	if _, ok := o["category"]; ok {
		return requireStr(o, "category")
	}
	return def
}

// bookGroup returns a recipe's group, "" for none.
// vanilla: Recipe.BookInfo.mapCodec
func bookGroup(o obj) string {
	if _, ok := o["group"]; ok {
		return requireStr(o, "group")
	}
	return ""
}

// craftingBase reads what shaped and shapeless recipes share: the result, the
// book category, the group and the notification flag.
// vanilla: CraftingRecipe.CraftingBookInfo, Recipe.CommonInfo
func (decoder ResourceDecoder) craftingBase(id string, o obj, into *registry.PackRecipes) registry.PackCrafting {
	category := bookCategory(o, "misc")
	categoryID, ok := craftingCategories[category]
	if !ok {
		fail("unknown crafting category %q", category)
	}
	result, count, patch := decoder.recipeResult(o)
	keepResult(into, id, patch)
	return registry.PackCrafting{
		Name: id, Category: categoryID, ResultID: result, ResultCount: count,
		Notify:    flag(o, "show_notification", true),
		GroupName: bookGroup(o),
	}
}

// shapelessRecipe reads one to nine ingredients, in any arrangement.
// vanilla: ShapelessRecipe.MAP_CODEC
func (decoder ResourceDecoder) shapelessRecipe(id string, o obj, into *registry.PackRecipes) registry.PackCrafting {
	recipe := decoder.craftingBase(id, o, into)
	ingredients := list(required(o, "ingredients"), "ingredients")
	if len(ingredients) < 1 || len(ingredients) > 9 {
		fail("a shapeless recipe takes 1 to 9 ingredients, not %d", len(ingredients))
	}
	for _, v := range ingredients {
		ing := decoder.recipeIngredient(v)
		recipe.Squares = append(recipe.Squares, bookSquare(ing))
		recipe.Items = append(recipe.Items, ing.Items)
	}
	return recipe
}

// shapedRecipe reads a pattern of up to 3x3 squares and the key naming what
// each symbol stands for. The pattern is shrunk to the smallest box holding
// every symbol, so the recipe matches wherever that box fits in the grid.
// vanilla: ShapedRecipePattern.Data, ShapedRecipePattern.unpack
func (decoder ResourceDecoder) shapedRecipe(id string, o obj, into *registry.PackRecipes) registry.PackCrafting {
	recipe := decoder.craftingBase(id, o, into)
	var rows [][]rune
	for _, row := range list(required(o, "pattern"), "pattern") {
		rows = append(rows, []rune(str(row, "a pattern row")))
	}
	if len(rows) == 0 || len(rows) > 3 {
		fail("a pattern takes 1 to 3 rows, not %d", len(rows))
	}
	for _, row := range rows {
		if len(row) > 3 || len(row) != len(rows[0]) {
			fail("each pattern row must be the same width, 3 at most")
		}
	}
	keys := map[rune]ingredient{}
	for symbol, v := range object(required(o, "key"), "key") {
		if r := []rune(symbol); len(r) != 1 || r[0] == ' ' {
			fail("%q is not a valid key symbol: one character, not a space", symbol)
		} else {
			keys[r[0]] = decoder.recipeIngredient(v)
		}
	}
	rows = shrinkPattern(rows)
	if len(rows) == 0 {
		fail("a pattern needs at least one symbol")
	}
	recipe.Shaped, recipe.Width, recipe.Height = true, int32(len(rows[0])), int32(len(rows))
	used := map[rune]bool{}
	for _, row := range rows {
		for _, symbol := range row {
			used[symbol] = true
			if symbol == ' ' {
				recipe.Squares = append(recipe.Squares, registry.BookIngredient{})
				recipe.Items = append(recipe.Items, nil)
				continue
			}
			ing, ok := keys[symbol]
			if !ok {
				fail("the pattern uses the symbol %q, which the key does not define", symbol)
			}
			recipe.Squares = append(recipe.Squares, bookSquare(ing))
			recipe.Items = append(recipe.Items, ing.Items)
		}
	}
	for symbol := range keys {
		if !used[symbol] {
			fail("the key defines the symbol %q, which the pattern does not use", symbol)
		}
	}
	return recipe
}

// shrinkPattern cuts the empty rows and columns off a pattern's edges; an
// all-empty pattern shrinks to nothing.
// vanilla: ShapedRecipePattern.shrink
func shrinkPattern(rows [][]rune) [][]rune {
	top, bottom, left, right := len(rows), -1, len(rows[0]), -1
	for y, row := range rows {
		for x, symbol := range row {
			if symbol != ' ' {
				top, bottom = min(top, y), max(bottom, y)
				left, right = min(left, x), max(right, x)
			}
		}
	}
	if bottom < 0 {
		return nil
	}
	out := make([][]rune, 0, bottom-top+1)
	for _, row := range rows[top : bottom+1] {
		out = append(out, row[left:right+1])
	}
	return out
}

// cookingRecipe reads a smelting, blasting, smoking or campfire recipe and its
// book display. The display's ingredient keeps a tag's items too, which
// placing the recipe in a furnace matches against. Its cookingtime is required;
// Codec.INT truncates a fraction and wraps a number past int range to its low
// 32 bits (Gson LazilyParsedNumber.intValue).
// vanilla: AbstractCookingRecipe.cookingMapCodec, AbstractCookingRecipe.display
func (decoder ResourceDecoder) cookingRecipe(id string, o obj, categories map[string]int32, into *registry.PackRecipes) registry.PackCooking {
	category := bookCategory(o, "misc")
	categoryID, ok := categories[category]
	if !ok {
		fail("unknown cooking category %q", category)
	}
	ing := decoder.recipeIngredient(required(o, "ingredient"))
	square := bookSquare(ing)
	if square.Tag != "" {
		square.Items = ing.Items
	}
	result, count, patch := decoder.recipeResult(o)
	keepResult(into, id, patch)
	experience := optNum(o, "experience", 0)
	required(o, "cookingtime")
	cookTime := int32(int64(requireNum(o, "cookingtime")))
	return registry.PackCooking{
		Name: id, Category: categoryID, Ingredient: square,
		ResultID: result, ResultCount: count, Duration: cookTime, Experience: float32(experience),
		GroupName: bookGroup(o),
	}
}

// trimPattern reads a smithing_trim's pattern, a trim_pattern registry name.
func trimPattern(v any) int32 {
	name := registry.Qualify(str(v, "pattern"))
	patterns, _ := registry.Get("minecraft:trim_pattern")
	id := patterns.ID(name)
	if id < 0 {
		fail("unknown trim pattern %s", name)
	}
	return id
}
