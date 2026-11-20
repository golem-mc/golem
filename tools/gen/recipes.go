package main

import (
	"fmt"
	"math"
	"path"
	"slices"
	"strings"

	"github.com/golem-mc/golem/tools/internal/gogen"
	"github.com/golem-mc/golem/tools/internal/vanilla"
)

func init() {
	register(generator{
		name: "recipes",
		doc:  "registry/recipes_gen.go: the crafting, cooking, smithing and brewing recipes with their ingredients expanded",
		run:  runRecipes,
	})
}

// The recipe files come from the data pack, item IDs from the data report, trim
// materials from the per-item component reports, and the trim registry order from
// registry/registries_gen.go.
//
// Only the types this server acts on are kept: crafting_shaped and
// crafting_shapeless, the four cooking kinds, smithing_transform, smithing_trim
// and brewing. A recipe whose result has data components is skipped, except the
// shapeless suspicious stew ones (kept as effect and duration), crafting_imbue
// (its result takes the source's potion_contents) and brewing (its result's
// potion).
//
// Ingredients are expanded to item ID sets here, following tags recursively
// through the data pack's item tags: a tag is fixed for the version, so following
// it at run time would only repeat this.

type recipeStewEffect struct {
	id       string
	duration int
}

// readStewEffect reads the only fixed result component these recipe tables model.
func readStewEffect(file string, result obj) (*recipeStewEffect, error) {
	components, has := result.obj("components")
	if !has || len(components) == 0 {
		return nil, nil
	}
	if len(components) != 1 || !components.has("minecraft:suspicious_stew_effects") {
		return nil, fmt.Errorf("%s: unsupported result components: %v", file, components)
	}
	effects := components.list("minecraft:suspicious_stew_effects")
	if len(effects) != 1 {
		return nil, fmt.Errorf("%s: want one stew effect, got %d", file, len(effects))
	}
	effect := asObj(effects[0], "a stew effect")
	// vanilla: SuspiciousStewEffects.Entry.CODEC
	return &recipeStewEffect{effect.str("id"), effect.intOr("duration", 160)}, nil
}

// defaultCookTime is the time a cooking recipe takes, in ticks, per recipe
// type; a recipe that says nothing takes the default of its own class.
// vanilla: SmeltingRecipe, BlastingRecipe, SmokingRecipe,
// CampfireCookingRecipe getDefaultCookingTime.
var defaultCookTime = map[string]int{
	"minecraft:smelting":         200,
	"minecraft:blasting":         100,
	"minecraft:smoking":          100,
	"minecraft:campfire_cooking": 600,
}

// keptRecipes are the types written; everything else in the data pack is
// skipped as not implemented.
var keptRecipes = []string{
	"minecraft:crafting_shaped", "minecraft:crafting_shapeless", "minecraft:crafting_imbue",
	"minecraft:crafting_transmute", "minecraft:crafting_dye",
	"minecraft:smelting", "minecraft:blasting", "minecraft:smoking", "minecraft:campfire_cooking",
	"minecraft:smithing_transform", "minecraft:smithing_trim", "minecraft:brewing",
}

// ingredientResolver turns the ingredients of the recipe files into item ID
// sets.
type ingredientResolver struct {
	items map[string]int
	dp    *vanilla.DataPack
}

func (r ingredientResolver) itemID(name string) int {
	id, ok := r.items[name]
	if !ok {
		fail("no such item: %s", name)
	}
	return id
}

// expandTag resolves #namespace:tag into the concrete items it collects. The
// data pack is the vanilla one, so every tag is a file of the minecraft
// namespace named after the tag itself.
func (r ingredientResolver) expandTag(name string, seen []string) []int {
	if slices.Contains(seen, name) {
		return nil
	}
	bare := strings.TrimPrefix(name, "#")
	bare = bare[strings.LastIndex(bare, ":")+1:]
	file := path.Join("minecraft/tags/item", bare+".json")
	b, err := dpRead(r.dp, file)
	if err != nil {
		fail("%v", err)
	}
	var ids []int
	for _, v := range decode(b, file).list("values") {
		value := asStr(v, "a tag value")
		if strings.HasPrefix(value, "#") {
			ids = append(ids, r.expandTag(value, append(slices.Clone(seen), name))...)
		} else {
			ids = append(ids, r.itemID(value))
		}
	}
	return sortedSet(ids)
}

func sortedSet(ids []int) []int {
	out := slices.Clone(ids)
	slices.Sort(out)
	return slices.Compact(out)
}

// ingredient expands one ingredient: an item, or an item list, or a #tag.
func (r ingredientResolver) ingredient(value any) []int {
	switch v := value.(type) {
	case []any:
		var ids []int
		for _, option := range v {
			ids = append(ids, r.ingredient(option)...)
		}
		return sortedSet(ids)
	case string:
		if strings.HasPrefix(v, "#") {
			return r.expandTag(v, nil)
		}
		return []int{r.itemID(v)}
	}
	fail("an ingredient of a shape the generator does not read: %v", value)
	return nil
}

// optional is the ingredient a smithing recipe's field names, expanded as
// above, or nil where the recipe leaves the field out: the codec of both
// smithing kinds allows a template and an addition to be absent.
func (r ingredientResolver) optional(recipe obj, field string) []int {
	if !recipe.has(field) {
		return nil
	}
	return r.ingredient(recipe[field])
}

func goInts(ids []int) string {
	parts := make([]string, len(ids))
	for i, id := range ids {
		parts[i] = fmt.Sprint(id)
	}
	return "{" + strings.Join(parts, ", ") + "}"
}

func ingredientLists(lists [][]int) string {
	parts := make([]string, len(lists))
	for i, ids := range lists {
		parts[i] = goInts(ids)
	}
	return strings.Join(parts, ", ")
}

// goField writes an ingredient as a struct field would hold it; nil is the
// ingredient the recipe leaves out.
func goField(ids []int, present bool) string {
	if !present {
		return "nil"
	}
	return "Ingredient" + goInts(ids)
}

type brewing struct {
	id                   string
	input, reagent       []int
	potion               int
	result, resultPotion int
}

// brewing reads a brewing recipe of the one shape the pack uses and the
// table holds: a bottle of one named potion, a bare reagent, and a result
// that is an item with a potion and nothing else.
// vanilla: BrewingRecipe.MAP_CODEC, PotionIngredient.MAP_CODEC
func (r ingredientResolver) brewing(file, id string, recipe obj, potions map[string]int) brewing {
	input := asObj(recipe.field("input"), "a brewing input")
	reagent := asObj(recipe.field("reagent"), "a brewing reagent")
	output := asObj(recipe.field("output"), "a brewing output")
	contents, _ := input.obj("potion_contents")
	components, _ := output.obj("components")
	made, _ := components.obj("minecraft:potion_contents")
	if !keysAre(recipe, "type", "input", "reagent", "output") || !keysAre(input, "item", "potion_contents") ||
		!keysAre(contents, "potions") || !keysAre(reagent, "item") || !keysAre(output, "id", "components") ||
		!keysAre(components, "minecraft:potion_contents") || !keysAre(made, "potion") {
		fail("%s: a brewing recipe of a shape the table cannot carry", file)
	}
	potionID := func(name string) int {
		id, ok := potions[name]
		if !ok {
			fail("no such potion: %s", name)
		}
		return id
	}
	return brewing{id, r.ingredient(input["item"]), r.ingredient(reagent["item"]),
		potionID(contents.str("potions")), r.itemID(output.str("id")), potionID(made.str("potion"))}
}

type smithingIngredients struct {
	template, base, addition []int
	hasTemplate, hasAddition bool
}

// imbue is a row of ImbueRecipes.
type imbue struct {
	id               string
	source, material []int
	result, count    int
}

// transmute and dye are the rows of TransmuteRecipes and DyeRecipes.
type transmute struct {
	id              string
	input, material []int
	lo, hi          int
	result, count   int
	addCount        bool
}
type dye struct {
	id            string
	target, dye   []int
	result, count int
}

// writeRewriting writes the imbue, transmute and dye tables.
func writeRewriting(f *gogen.File, imbues []imbue, transmutes []transmute, dyes []dye) {
	f.WriteString("// ImbueRecipes holds every crafting_imbue recipe of the data pack.\n")
	f.WriteString("var ImbueRecipes = []ImbueRecipe{\n")
	for _, s := range imbues {
		f.Printf("\t{%q, Ingredient%s, Ingredient%s, %d, %d},\n", s.id, goInts(s.source), goInts(s.material), s.result, s.count)
	}
	f.WriteString("}\n\n")
	f.WriteString("// TransmuteRecipes holds every crafting_transmute recipe of the data pack.\n")
	f.WriteString("var TransmuteRecipes = []TransmuteRecipe{\n")
	for _, s := range transmutes {
		f.Printf("\t{%q, Ingredient%s, Ingredient%s, %d, %d, %d, %d, %t},\n", s.id, goInts(s.input), goInts(s.material), s.lo, s.hi, s.result, s.count, s.addCount)
	}
	f.WriteString("}\n\n")
	f.WriteString("// DyeRecipes holds every crafting_dye recipe of the data pack.\n")
	f.WriteString("var DyeRecipes = []DyeRecipe{\n")
	for _, s := range dyes {
		f.Printf("\t{%q, Ingredient%s, Ingredient%s, %d, %d},\n", s.id, goInts(s.target), goInts(s.dye), s.result, s.count)
	}
	f.WriteString("}\n\n")
}

// rewriting reads a crafting_transmute or crafting_dye recipe. A result with
// components is not modelled, and an empty id keeps the input's item (0).
// vanilla: TransmuteResult.MAP_CODEC
func (r ingredientResolver) rewriting(file, id, kind string, recipe, result obj) (transmute, dye, error) {
	if result.has("components") {
		return transmute{}, dye{}, fmt.Errorf("%s: a %s result with components is not modelled", file, kind)
	}
	resultID := 0
	if result.has("id") {
		resultID = r.itemID(result.str("id"))
	}
	count := result.intOr("count", 1)
	if kind == "minecraft:crafting_dye" {
		return transmute{}, dye{id, r.ingredient(recipe.field("target")), r.ingredient(recipe.field("dye")), resultID, count}, nil
	}
	lo, hi := materialBounds(recipe)
	return transmute{id, r.ingredient(recipe.field("input")), r.ingredient(recipe.field("material")),
		lo, hi, resultID, count, recipe.boolOr("add_material_count_to_result", false)}, dye{}, nil
}

func runRecipes(e *env) error {
	registries, err := e.ref.Registries()
	if err != nil {
		return err
	}
	itemEntries := registries["minecraft:item"].Entries
	items := make(map[string]int, itemEntries.Len())
	for name, entry := range itemEntries.All() {
		items[name] = entry.ProtocolID
	}
	trimPatterns, err := indexedRegistry(e, "minecraft:trim_pattern")
	if err != nil {
		return err
	}
	trimMaterials, err := indexedRegistry(e, "minecraft:trim_material")
	if err != nil {
		return err
	}
	dp, err := e.ref.DataPack()
	if err != nil {
		return err
	}
	defer dp.Close()
	r := ingredientResolver{items, dp}
	potions := map[string]int{}
	for name, entry := range registries["minecraft:potion"].Entries.All() {
		potions[name] = entry.ProtocolID
	}
	componentsDir := e.ref.ReportPath("minecraft/components/item")

	// trimMaterialOf is the trim material an item provides, from the
	// minecraft:provides_trim_material component the data report carries for
	// it; no such component is no trim material.
	trimMaterialOf := func(item string) int {
		file := path.Join(componentsDir, item[strings.LastIndex(item, ":")+1:]+".json")
		components, _ := readObj(file).obj("components")
		provides, present := components["minecraft:provides_trim_material"]
		if !present {
			fail("%s provides no trim material, want one for the smithing table", item)
		}
		name, _ := provides.(string)
		id, ok := trimMaterials[name]
		if !ok {
			fail("no trim material %v in the registry", provides)
		}
		return id
	}

	// The trim materials, one per item the trim_materials tag collects: the
	// additions a smithing_trim may name. An item in the tag providing no
	// material stops the generation rather than being guessed about.
	trimMaterialItems := map[int]int{}
	tagFile := "minecraft/tags/item/trim_materials.json"
	tagBytes, err := dpRead(dp, tagFile)
	if err != nil {
		return err
	}
	for _, v := range decode(tagBytes, tagFile).list("values") {
		value := asStr(v, "a tag value")
		if strings.HasPrefix(value, "#") {
			return fmt.Errorf("the trim_materials tag names the nested tag %s, which this generator does not read", value)
		}
		trimMaterialItems[r.itemID(value)] = trimMaterialOf(value)
	}

	type shaped struct {
		id            string
		width, height int
		cells         [][]int
		result, count int
	}
	type shapeless struct {
		id            string
		ingredients   [][]int
		result, count int
	}
	type stewRecipe struct {
		id            string
		ingredients   [][]int
		result, count int
		effect        string
		duration      int
	}
	type cooking struct {
		id            string
		ingredient    []int
		result, count int
		experience    float64
		cookTime      int
	}
	type smithingTransform struct {
		id string
		smithingIngredients
		result, count int
	}
	type smithingTrim struct {
		id string
		smithingIngredients
		pattern int
	}
	var (
		shapedRecipes    []shaped
		shapelessRecipes []shapeless
		stew             []stewRecipe
		imbues           []imbue
		transmutes       []transmute
		dyes             []dye
		transforms       []smithingTransform
		trims            []smithingTrim
		brews            []brewing
		skipped          = map[string]int{}
	)
	cookingKinds := []string{"minecraft:smelting", "minecraft:blasting", "minecraft:smoking", "minecraft:campfire_cooking"}
	cookingRecipes := map[string][]cooking{}

	files, err := vanilla.Files(dp, "minecraft/recipe", ".json")
	if err != nil {
		return err
	}
	for _, rel := range files {
		file := path.Join("minecraft/recipe", rel)
		id := "minecraft:" + strings.TrimSuffix(rel, ".json")
		b, err := dpRead(dp, file)
		if err != nil {
			return err
		}
		recipe := decode(b, file)
		kind := recipe.str("type")
		// The special recipe files carry no result at all: the client
		// computes what they make, so there is nothing to read. The smithing
		// kinds have theirs read below; a trim makes its base the result,
		// transformed by the ingredients alone.
		if !slices.Contains(keptRecipes, kind) {
			skipped[kind]++
			continue
		}
		smithing := func() smithingIngredients {
			s := smithingIngredients{
				template: r.optional(recipe, "template"), hasTemplate: recipe.has("template"),
				base:     r.ingredient(recipe.field("base")),
				addition: r.optional(recipe, "addition"), hasAddition: recipe.has("addition"),
			}
			return s
		}
		if kind == "minecraft:smithing_trim" {
			pattern := recipe.str("pattern")
			patternID, ok := trimPatterns[pattern]
			if !ok {
				return fmt.Errorf("no trim pattern %s in the registry", pattern)
			}
			trims = append(trims, smithingTrim{id, smithing(), patternID})
			continue
		}
		if kind == "minecraft:smithing_transform" {
			result := asObj(recipe.field("result"), "a result")
			if result.has("components") {
				// A result carrying data components cannot be handed to a
				// player (see protocol.Buffer.ReadSlot), so the recipe is
				// skipped, though the data pack carries none.
				skipped[kind+" with components"]++
				continue
			}
			transforms = append(transforms, smithingTransform{id, smithing(), r.itemID(result.str("id")), result.intOr("count", 1)})
			continue
		}
		if kind == "minecraft:brewing" {
			brews = append(brews, r.brewing(file, id, recipe, potions))
			continue
		}
		result := asObj(recipe.field("result"), "a result")
		if kind == "minecraft:crafting_imbue" {
			imbues = append(imbues, imbue{id, r.ingredient(recipe.field("source")), r.ingredient(recipe.field("material")),
				r.itemID(result.str("id")), result.intOr("count", 1)})
			continue
		}
		if kind == "minecraft:crafting_transmute" || kind == "minecraft:crafting_dye" {
			t, d, err := r.rewriting(file, id, kind, recipe, result)
			if err != nil {
				return err
			}
			if kind == "minecraft:crafting_dye" {
				dyes = append(dyes, d)
			} else {
				transmutes = append(transmutes, t)
			}
			continue
		}
		if kind == "minecraft:crafting_shapeless" && result.has("components") {
			effect, err := readStewEffect(file, result)
			if err != nil {
				return err
			}
			if effect != nil {
				var ingredients [][]int
				for _, v := range recipe.list("ingredients") {
					ingredients = append(ingredients, r.ingredient(v))
				}
				stew = append(stew, stewRecipe{id, ingredients, r.itemID(result.str("id")), result.intOr("count", 1), effect.id, effect.duration})
				continue
			}
		}

		if result.has("components") {
			// Any other component is not read from a recipe.
			skipped[kind+" with components"]++
			continue
		}
		resultID, resultCount := r.itemID(result.str("id")), result.intOr("count", 1)
		switch kind {
		case "minecraft:crafting_shaped":
			rows := shrunkPattern(recipe)
			key := map[rune][]int{}
			keyObj, _ := recipe.obj("key")
			for char, value := range keyObj {
				key[[]rune(char)[0]] = r.ingredient(value)
			}
			var cells [][]int
			for _, row := range rows {
				for _, char := range row {
					cells = append(cells, key[char])
				}
			}
			shapedRecipes = append(shapedRecipes, shaped{id, len(rows[0]), len(rows), cells, resultID, resultCount})
		case "minecraft:crafting_shapeless":
			// A shapeless list may hold the same ingredient twice, which is
			// one item in each of two squares, so the duplicates are kept.
			var ingredients [][]int
			for _, v := range recipe.list("ingredients") {
				ingredients = append(ingredients, r.ingredient(v))
			}
			shapelessRecipes = append(shapelessRecipes, shapeless{id, ingredients, resultID, resultCount})
		default:
			// The recipe's id is the resource location the vanilla server
			// loads the file under (the minecraft namespace plus the file's
			// name) and is what a furnace's RecipesUsed compound keys its
			// counts by.
			cookingRecipes[kind] = append(cookingRecipes[kind], cooking{
				id, r.ingredient(recipe.field("ingredient")),
				resultID, resultCount, recipe.numOr("experience", 0), recipe.intOr("cookingtime", defaultCookTime[kind])})
		}
	}

	f := e.file("registry", "from the vanilla {version} data pack's recipes")
	f.WriteString(recipeTypes)
	f.WriteString("// ShapedRecipes holds every crafting_shaped recipe of the data pack.\n")
	f.WriteString("var ShapedRecipes = []ShapedRecipe{\n")
	for _, s := range shapedRecipes {
		f.Printf("\t{%q, %d, %d, []Ingredient{%s}, %d, %d},\n", s.id, s.width, s.height, ingredientLists(s.cells), s.result, s.count)
	}
	f.WriteString("}\n\n")
	f.WriteString("// ShapelessRecipes holds every crafting_shapeless recipe of the data pack.\n")
	f.WriteString("var ShapelessRecipes = []ShapelessRecipe{\n")
	for _, s := range shapelessRecipes {
		f.Printf("\t{%q, []Ingredient{%s}, %d, %d},\n", s.id, ingredientLists(s.ingredients), s.result, s.count)
	}
	f.WriteString("}\n\n")
	f.WriteString("// StewRecipes holds every suspicious stew recipe of the data pack.\n")
	f.WriteString("var StewRecipes = []StewRecipe{\n")
	for _, s := range stew {
		f.Printf("\t{%q, []Ingredient{%s}, %d, %d, \"%s\", %d},\n", s.id, ingredientLists(s.ingredients), s.result, s.count, s.effect, s.duration)
	}
	f.WriteString("}\n\n")
	writeRewriting(f, imbues, transmutes, dyes)
	for i, kind := range cookingKinds {
		variable := []string{"SmeltingRecipes", "BlastingRecipes", "SmokingRecipes", "CampfireCookingRecipes"}[i]
		f.Printf("// %s holds every %s recipe of the data pack.\n", variable, kind)
		f.Printf("var %s = []CookingRecipe{\n", variable)
		for _, c := range cookingRecipes[kind] {
			// The ingredient is a struct field rather than a slice element
			// here, so its type cannot be elided the way the other lists can.
			f.Printf("\t{\"%s\", Ingredient%s, %d, %d, %s, %d},\n", c.id, goInts(c.ingredient), c.result, c.count,
				gogen.Float(c.experience), c.cookTime)
		}
		f.WriteString("}\n\n")
	}
	// The smithing tables: both kinds take their ingredients whole, the way a
	// struct field does; nil is the ingredient the recipe leaves out.
	f.WriteString("// SmithingTransformRecipes holds every smithing_transform recipe of the data pack.\n")
	f.WriteString("var SmithingTransformRecipes = []SmithingTransformRecipe{\n")
	for _, s := range transforms {
		f.Printf("\t{%q, %s, %s, %s, %d, %d},\n", s.id, goField(s.template, s.hasTemplate), goField(s.base, true),
			goField(s.addition, s.hasAddition), s.result, s.count)
	}
	f.WriteString("}\n\n")
	f.WriteString("// SmithingTrimRecipes holds every smithing_trim recipe of the data pack.\n")
	f.WriteString("var SmithingTrimRecipes = []SmithingTrimRecipe{\n")
	for _, s := range trims {
		f.Printf("\t{%q, %s, %s, %s, %d},\n", s.id, goField(s.template, s.hasTemplate), goField(s.base, true),
			goField(s.addition, s.hasAddition), s.pattern)
	}
	f.WriteString("}\n\n")
	f.WriteString("// BrewingRecipes holds every brewing recipe of the data pack.\n")
	f.WriteString("var BrewingRecipes = []BrewingRecipe{\n")
	for _, b := range brews {
		f.Printf("\t{%q, Ingredient%s, %d, Ingredient%s, %d, %d},\n", b.id, goInts(b.input), b.potion, goInts(b.reagent), b.result, b.resultPotion)
	}
	f.WriteString("}\n\n")
	f.WriteString(`// TrimMaterials maps each item that names a trim material in its
// minecraft:provides_trim_material component to that material's ID in the
// minecraft:trim_material registry: the addition of a smithing_trim takes
// the material of its trim from the item it was given.
`)
	f.WriteString("var TrimMaterials = map[int32]int32{\n")
	ids := make([]int, 0, len(trimMaterialItems))
	for id := range trimMaterialItems {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	for _, id := range ids {
		f.Printf("\t%d: %d,\n", id, trimMaterialItems[id])
	}
	f.WriteString("}\n\n")

	for _, name := range sortedKeys(skipped) {
		e.logf("recipes: skipped %d of %s", skipped[name], name)
	}
	e.logf("recipes: %d shaped, %d shapeless, %d smelting, %d blasting, %d smoking, %d smithing_transform, %d smithing_trim, %d brewing, %d trim materials",
		len(shapedRecipes), len(shapelessRecipes), len(cookingRecipes["minecraft:smelting"]), len(cookingRecipes["minecraft:blasting"]),
		len(cookingRecipes["minecraft:smoking"]), len(transforms), len(trims), len(brews), len(trimMaterialItems))
	return e.emit("registry/recipes_gen.go", f)
}

// indexedRegistry maps the entries of a synchronized registry to their IDs,
// the position registry/registries_gen.go gives them.
func indexedRegistry(e *env, registry string) (map[string]int, error) {
	names, err := syncedEntries(e, registry)
	if err != nil {
		return nil, err
	}
	out := make(map[string]int, len(names))
	for id, name := range names {
		out[name] = id
	}
	return out, nil
}

// shrunkPattern reads a shaped recipe's pattern rows with the blank edge rows
// and columns cut off, so the recipe's size is that of its symbols' box.
// vanilla: ShapedRecipePattern.shrink
func shrunkPattern(recipe obj) []string {
	var rows []string
	for _, row := range recipe.list("pattern") {
		rows = append(rows, asStr(row, "a pattern row"))
	}
	top, bottom, left, right := len(rows), -1, math.MaxInt, -1
	for y, row := range rows {
		if strings.Trim(row, " ") == "" {
			continue
		}
		top, bottom = min(top, y), max(bottom, y)
		left = min(left, len(row)-len(strings.TrimLeft(row, " ")))
		right = max(right, len(strings.TrimRight(row, " "))-1)
	}
	if bottom < 0 {
		fail("a shaped recipe's pattern has no symbol")
	}
	rows = rows[top : bottom+1]
	for i, row := range rows {
		rows[i] = row[left : right+1]
	}
	return rows
}
