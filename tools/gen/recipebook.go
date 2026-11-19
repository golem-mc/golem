package main

import (
	"encoding/binary"
	"fmt"
	"path"
	"slices"
	"strings"

	"github.com/golem-mc/golem/tools/internal/gogen"
	"github.com/golem-mc/golem/tools/internal/vanilla"
)

func init() {
	register(generator{
		name: "recipebook",
		doc:  "registry/recipebook_gen.go: the recipe book's property sets, stonecutter list and crafting displays",
		run:  runRecipeBook,
	})
}

// The recipe book packets a client gets at join: the property sets its filters
// test (the items a furnace takes, the smithing bases, ...), the stonecutter list
// and the crafting displays. Tags are expanded into item IDs.
//
// Vanilla builds each property set from the ingredients of one recipe kind
// (furnace from smelting, blast furnace from blasting, smoker and campfire from
// theirs, the three smithing sets from the template, base and addition).
//
// Which recipes a player starts with is written in the data pack's advancements,
// not its recipes: a recipe-unlock advancement whose only criterion is
// minecraft:tick is a starting recipe. Display IDs count the recipes that have
// one in alphabetical order; the special recipes the client computes and the
// decorated pot have none.
//
// Deliberate difference from vanilla: the property sets travel in report order
// (sets by name, items by item registry ID). Vanilla sends them in an order that
// changes with every server run (hash-salted iteration) and the client only looks
// them up, so a stable order keeps the generated file stable. The stonecutter
// list keeps vanilla's alphabetical order.

// recipePropertySets are the property sets vanilla builds, named as the client
// looks them up, with the recipes that feed each: a recipe type, a smithing
// field, or a brewing recipe's bottle or reagent. The names and the extraction are vanilla's own (RecipeManager's
// property set table).
var recipePropertySets = []struct{ name, recipeType, smithing, brewing string }{
	{"minecraft:blast_furnace_input", "minecraft:blasting", "", ""},
	{"minecraft:brewing_input", "", "", "input"},
	{"minecraft:brewing_reagent", "", "", "reagent"},
	{"minecraft:campfire_input", "minecraft:campfire_cooking", "", ""},
	{"minecraft:furnace_input", "minecraft:smelting", "", ""},
	{"minecraft:smithing_addition", "", "addition", ""},
	{"minecraft:smithing_base", "", "base", ""},
	{"minecraft:smithing_template", "", "template", ""},
	{"minecraft:smoker_input", "minecraft:smoking", "", ""},
}

// The recipe types whose class defines no display, so they take a display ID
// from no recipe: the special recipes the client computes itself, the
// decorated pot and brewing. Transmutation can have several displays.
const noDisplayPrefix = "minecraft:crafting_special_"

func hasDisplay(recipeType string) bool {
	return !strings.HasPrefix(recipeType, noDisplayPrefix) && recipeType != "minecraft:crafting_decorated_pot" &&
		recipeType != "minecraft:brewing"
}

// bookCategories maps a recipe type onto its book category, for the types whose
// data pack file does not carry it. Shaped and shapeless recipes carry their own
// (misc when absent); cooking recipes land in the book of the block that cooks
// them; stonecutter and smithing categories are the type's alone. Anything else is
// refused.
var bookCategories = map[string]string{
	"minecraft:smelting":           "furnace_%s",
	"minecraft:blasting":           "blast_furnace_%s",
	"minecraft:smoking":            "smoker_food",
	"minecraft:campfire_cooking":   "campfire",
	"minecraft:stonecutting":       "stonecutter",
	"minecraft:smithing_transform": "smithing",
	"minecraft:smithing_trim":      "smithing",
}

type bookSquare struct {
	tag   string
	items []int
}

type bookComponent struct {
	kind string
	data []byte
}

type craftingDisplay struct {
	id, group, category, width, height, result, resultCount int
	name                                                    string
	fresh, shaped, notify                                   bool
	squares                                                 []bookSquare
	resultComponents                                        []bookComponent
	slots                                                   *craftingSlots
}

type furnaceDisplay struct {
	id, kind, group, category, result, resultCount, duration int
	name                                                     string
	experience                                               float64
	ingredient                                               bookSquare
}

type namedRecipe struct {
	id     string
	recipe obj
}

type stonecut struct {
	id                   string
	input, result, count int
}

// unlockAdvancement is a recipe-unlock advancement, as far as the book reads it.
type unlockAdvancement struct {
	Criteria vanilla.Ordered[obj] `json:"criteria"`
	Rewards  struct {
		Recipes []string `json:"recipes"`
	} `json:"rewards"`
}

// recipeBookReader holds the registries and the data pack the tables are read
// from.
type recipeBookReader struct {
	dp           *vanilla.DataPack
	items        map[string]int
	itemNames    map[int]string
	categories   map[string]int
	freshIDs     map[string]bool
	effects      map[string]int
	dyeComponent int
}

func (r *recipeBookReader) itemID(name string) int {
	id, ok := r.items[name]
	if !ok {
		fail("no such item: %s", name)
	}
	return id
}

// expandTag resolves #namespace:tag into the concrete items it collects. The
// data pack is the vanilla one, so every tag is a file of the minecraft
// namespace named after the tag itself.
func (r *recipeBookReader) expandTag(name string, seen []string) []int {
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
	return ids
}

// expand is one ingredient: an item, or an item list, or a #tag.
func (r *recipeBookReader) expand(value any) []int {
	switch v := value.(type) {
	case []any:
		var ids []int
		for _, option := range v {
			ids = append(ids, r.expand(option)...)
		}
		return ids
	case string:
		if strings.HasPrefix(v, "#") {
			return r.expandTag(v, nil)
		}
		return []int{r.itemID(v)}
	}
	fail("an ingredient of a shape the generator does not read: %v", value)
	return nil
}

func (r *recipeBookReader) bookCategory(recipe obj) int {
	recipeType := recipe.str("type")
	var name string
	if strings.HasPrefix(recipeType, "minecraft:crafting_") {
		// The crafting categories name their registry entries in full; the
		// short forms the data pack also takes (building, redstone, equipment,
		// misc) are its own prefixes of them.
		// vanilla: RecipeBookCategories
		category := recipe.strOr("category", "misc")
		if category == "building" {
			category = "building_blocks"
		}
		name = "minecraft:crafting_" + category
	} else {
		pattern, ok := bookCategories[recipeType]
		if !ok {
			fail("no book category known for recipe type %s", recipeType)
		}
		name = "minecraft:" + strings.Replace(pattern, "%s", recipe.strOr("category", "misc"), 1)
	}
	id, ok := r.categories[name]
	if !ok {
		fail("no such recipe book category: %s", name)
	}
	return id
}

// propertySets gathers, for each set, every item its recipes' ingredients
// accept, tags expanded; the order within the set is the item registry's.
func (r *recipeBookReader) propertySets(recipes []namedRecipe) map[string]map[int]bool {
	sets := map[string]map[int]bool{}
	for _, ps := range recipePropertySets {
		sets[ps.name] = map[int]bool{}
	}
	for _, nr := range recipes {
		for _, ps := range recipePropertySets {
			var source any
			switch {
			case ps.recipeType != "":
				if nr.recipe.str("type") != ps.recipeType {
					continue
				}
				source = nr.recipe.field("ingredient")
			case ps.brewing != "":
				if nr.recipe.str("type") != "minecraft:brewing" {
					continue
				}
				source = asObj(nr.recipe.field(ps.brewing), "a brewing ingredient").field("item")
			case strings.Contains(nr.recipe.str("type"), "smithing"):
				source = nr.recipe.field(ps.smithing)
			default:
				continue
			}
			for _, id := range r.expand(source) {
				sets[ps.name][id] = true
			}
		}
	}
	return sets
}

// stonecutter reads the stonecutter recipes, in the alphabetical recipe order
// vanilla sends.
func (r *recipeBookReader) stonecutter(recipes []namedRecipe) ([]stonecut, error) {
	var out []stonecut
	for _, nr := range recipes {
		if nr.recipe.str("type") != "minecraft:stonecutting" {
			continue
		}
		ingredient, isString := nr.recipe.field("ingredient").(string)
		if !isString || strings.HasPrefix(ingredient, "#") {
			return nil, fmt.Errorf("stonecutter recipe %s cuts a tag or a list; the table holds single items only", nr.id)
		}
		result := asObj(nr.recipe.field("result"), "a result")
		if result.has("components") {
			return nil, fmt.Errorf("stonecutter recipe %s produces a stack with components; the table cannot carry one", nr.id)
		}
		out = append(out, stonecut{"minecraft:" + nr.id, r.itemID(ingredient), r.itemID(result.str("id")), result.intOr("count", 1)})
	}
	return out, nil
}

// loadAdvancements reads the recipe-unlock advancements for the recipes one
// asks nothing but a first tick of (the crafting table's), which is the book a
// player starts with; the rest are unlocked by the advancements' rewards.
func (r *recipeBookReader) loadAdvancements() error {
	const dir = "minecraft/advancement/recipes"
	files, err := vanilla.Files(r.dp, dir, ".json")
	if err != nil {
		return err
	}
	for _, rel := range files {
		var adv unlockAdvancement
		if err := vanilla.ReadJSON(r.dp, path.Join(dir, rel), &adv); err != nil {
			return err
		}
		for _, recipe := range adv.Rewards.Recipes {
			if !strings.Contains(recipe, ":") {
				recipe = "minecraft:" + recipe
			}
			for _, criterion := range adv.Criteria.All() {
				if criterion.strOr("trigger", "") == "minecraft:tick" {
					r.freshIDs[recipe] = true
					break
				}
			}
		}
	}
	return nil
}

// squareOf is one ingredient as the wire and the matcher see it: a tag the
// client resolves, or the item list in the file's order, which the display
// sends.
// vanilla: Ingredient.display
func (r *recipeBookReader) squareOf(value any) bookSquare {
	if s, ok := value.(string); ok && strings.HasPrefix(s, "#") {
		return bookSquare{s[1:], r.expand(value)}
	}
	return bookSquare{"", r.expand(value)}
}

// squares reads a crafting recipe's squares and, for a shaped one, its size.
func (r *recipeBookReader) squares(recipe obj, shaped bool) (squares []bookSquare, width, height int) {
	if !shaped {
		// The placing of a shapeless recipe runs it over the whole grid, so
		// its ingredients need no shape of their own.
		for _, value := range recipe.list("ingredients") {
			squares = append(squares, r.squareOf(value))
		}
		return squares, 0, 0
	}
	key, _ := recipe.obj("key")
	rows := shrunkPattern(recipe)
	for _, row := range rows {
		for _, char := range row {
			value, present := key[string(char)]
			if char == ' ' || !present {
				squares = append(squares, bookSquare{}) // a square the recipe leaves out
			} else {
				squares = append(squares, r.squareOf(value))
			}
		}
	}
	return squares, len(rows[0]), len(rows)
}

// displayIDOf counts the recipes with a display before id in the alphabetical
// scan.
func displayIDOf(recipes []namedRecipe, id string) int {
	n := 0
	for _, other := range recipes {
		if other.id < id && hasDisplay(other.recipe.str("type")) {
			n += recipeDisplayCount(other.recipe)
		}
	}
	return n
}

// cookingKinds are the recipe types of the furnace displays, in the order of
// registry.FurnaceDisplay.Kind and PackRecipes.Cooking.
var cookingKinds = []string{"minecraft:smelting", "minecraft:blasting", "minecraft:smoking", "minecraft:campfire_cooking"}

// recipeGroupIDs numbers the recipes' group names by first appearance, and
// returns them in ID order.
func recipeGroupIDs(recipes []namedRecipe) (ids map[string]int, names []string) {
	ids = map[string]int{}
	for _, nr := range recipes {
		if group := nr.recipe.strOr("group", ""); group != "" {
			if _, seen := ids[group]; !seen {
				ids[group] = len(ids)
				names = append(names, group)
			}
		}
	}
	return ids, names
}

// furnaceDisplays builds one furnace display per cooking recipe, in display ID
// order. The group IDs are the crafting displays' scan.
// vanilla: AbstractCookingRecipe.display
func (r *recipeBookReader) furnaceDisplays(recipes []namedRecipe) ([]furnaceDisplay, error) {
	groupIDs, _ := recipeGroupIDs(recipes)
	var out []furnaceDisplay
	for _, nr := range recipes {
		kind := slices.Index(cookingKinds, nr.recipe.str("type"))
		if kind < 0 {
			continue
		}
		result := asObj(nr.recipe.field("result"), "a result")
		if result.has("components") {
			return nil, fmt.Errorf("cooking recipe %s produces a stack with components", nr.id)
		}
		group, hasGroup := groupIDs[nr.recipe.strOr("group", "")]
		if !hasGroup {
			group = -1
		}
		out = append(out, furnaceDisplay{
			id: displayIDOf(recipes, nr.id), name: "minecraft:" + nr.id, kind: kind,
			group: group, category: r.bookCategory(nr.recipe),
			ingredient: r.squareOf(nr.recipe.field("ingredient")),
			result:     r.itemID(result.str("id")), resultCount: result.intOr("count", 1),
			duration:   nr.recipe.intOr("cookingtime", defaultCookTime[nr.recipe.str("type")]),
			experience: nr.recipe.numOr("experience", 0),
		})
	}
	return out, nil
}

// displays builds one crafting display per craftable recipe: its squares as the
// wire's tag-or-items and as expanded item IDs, including the result component patch.
func (r *recipeBookReader) displays(recipes []namedRecipe) (displays []craftingDisplay, err error) {
	// The group IDs vanilla's all-display scan assigns: every recipe's group
	// gets the number of its first appearance in the alphabetical scan,
	// counted over all recipes (displays or not, since the scan reads the
	// group before it reaches for the recipe's displays). The empty group
	// names none. Entries without one carry -1.
	groupIDs, _ := recipeGroupIDs(recipes)
	for _, nr := range recipes {
		recipeType := nr.recipe.str("type")
		if !strings.HasPrefix(recipeType, "minecraft:crafting_") || !hasDisplay(recipeType) {
			continue
		}
		if recipeType != "minecraft:crafting_shaped" && recipeType != "minecraft:crafting_shapeless" {
			dynamic, err := r.dynamicDisplays(recipes, nr)
			if err != nil {
				return nil, err
			}
			displays = append(displays, dynamic...)
			continue
		}
		displayID := displayIDOf(recipes, nr.id)
		result := asObj(nr.recipe.field("result"), "a result")
		effect, err := readStewEffect(nr.id, result)
		if err != nil {
			return nil, err
		}
		var components []bookComponent
		if effect != nil {
			effectID, ok := r.effects[effect.id]
			if !ok {
				return nil, fmt.Errorf("%s: unknown stew effect %s", nr.id, effect.id)
			}
			// vanilla: SuspiciousStewEffects.STREAM_CODEC
			data := []byte{1}
			data = binary.AppendUvarint(data, uint64(effectID))
			data = binary.AppendUvarint(data, uint64(uint32(effect.duration)))
			components = []bookComponent{{"SuspiciousStew", data}}
		}
		shaped := recipeType == "minecraft:crafting_shaped"
		squares, width, height := r.squares(nr.recipe, shaped)
		group, hasGroup := groupIDs[nr.recipe.strOr("group", "")]
		if !hasGroup {
			group = -1
		}
		displays = append(displays, craftingDisplay{
			id: displayID, name: "minecraft:" + nr.id, fresh: r.freshIDs["minecraft:"+nr.id],
			group: group, category: r.bookCategory(nr.recipe), shaped: shaped,
			width: width, height: height, squares: squares,
			result: r.itemID(result.str("id")), resultCount: result.intOr("count", 1),
			notify:           nr.recipe.boolOr("show_notification", true),
			resultComponents: components,
		})
	}
	return displays, nil
}

func runRecipeBook(e *env) error {
	registries, err := e.ref.Registries()
	if err != nil {
		return err
	}
	dp, err := e.ref.DataPack()
	if err != nil {
		return err
	}
	defer dp.Close()
	r := &recipeBookReader{
		dp: dp, items: map[string]int{}, itemNames: map[int]string{}, categories: map[string]int{},
		freshIDs: map[string]bool{}, effects: map[string]int{}, dyeComponent: -1,
	}
	for name, entry := range registries["minecraft:mob_effect"].Entries.All() {
		r.effects[name] = entry.ProtocolID
	}
	dye, ok := registries["minecraft:data_component_type"].Entries.Get("minecraft:dye")
	if ok {
		r.dyeComponent = dye.ProtocolID
	}
	for name, entry := range registries["minecraft:item"].Entries.All() {
		r.items[name] = entry.ProtocolID
		r.itemNames[entry.ProtocolID] = name
	}
	for name, entry := range registries["minecraft:recipe_book_category"].Entries.All() {
		r.categories[name] = entry.ProtocolID
	}

	// The recipes of the data pack, in the alphabetical order of their files,
	// which is the order the vanilla server loads them in (its recipe map is
	// keyed by a sorted tree map).
	var recipes []namedRecipe
	files, err := vanilla.Files(dp, "minecraft/recipe", ".json")
	if err != nil {
		return err
	}
	for _, rel := range files {
		b, err := dpRead(dp, path.Join("minecraft/recipe", rel))
		if err != nil {
			return err
		}
		recipes = append(recipes, namedRecipe{strings.TrimSuffix(rel, ".json"), decode(b, rel)})
	}
	sets := r.propertySets(recipes)
	stonecutter, err := r.stonecutter(recipes)
	if err != nil {
		return err
	}
	if err := r.loadAdvancements(); err != nil {
		return err
	}
	displays, err := r.displays(recipes)
	if err != nil {
		return err
	}
	furnaces, err := r.furnaceDisplays(recipes)
	if err != nil {
		return err
	}
	trimPatterns, err := indexedRegistry(e, "minecraft:trim_pattern")
	if err != nil {
		return err
	}
	_, groupNames := recipeGroupIDs(recipes)

	version := e.ref.Version
	f := e.file("registry", "from the vanilla {version} data pack's recipes")
	f.WriteString("import \"github.com/golem-mc/golem/internal/protocol\"\n\n")
	f.WriteString(strings.ReplaceAll(recipeBookTypes, "{version}", version))
	for _, ps := range recipePropertySets {
		ids := make([]int, 0, len(sets[ps.name]))
		for id := range sets[ps.name] {
			ids = append(ids, id)
		}
		slices.Sort(ids)
		f.Printf("\t{\"%s\", []int32%s},\n", ps.name, goInts(ids))
	}
	f.WriteString("}\n\n// RecipeGroupNames holds the built-in recipe group names, indexed by group ID.\nvar RecipeGroupNames = []string{\n")
	for _, name := range groupNames {
		f.Printf("\t%q,\n", name)
	}
	f.WriteString(strings.ReplaceAll(recipeBookStonecutter, "{version}", version))
	for _, s := range stonecutter {
		f.Printf("\t{%q, %d, %d, %d},\n", s.id, s.input, s.result, s.count)
	}
	f.WriteString(strings.ReplaceAll(recipeBookDisplays, "{version}", version))
	for _, d := range displays {
		f.Printf("\t%s,\n", goDisplay(d))
	}
	f.WriteString(strings.ReplaceAll(recipeBookFurnaceDisplays, "{version}", version))
	for _, d := range furnaces {
		f.Printf("\t{%d, %q, %d, %d, %d, BookIngredient%s, Ingredient%s, %d, %d, %d, %s},\n", d.id, d.name, d.kind, d.group, d.category,
			goSquare(d.ingredient), goInts(d.ingredient.items), d.result, d.resultCount, d.duration, gogen.Float(d.experience))
	}
	f.WriteString(strings.ReplaceAll(recipeBookStonecutterDisplays, "{version}", version))
	for _, nr := range recipes {
		if nr.recipe.str("type") != "minecraft:stonecutting" {
			continue
		}
		result := asObj(nr.recipe.field("result"), "a result")
		f.Printf("\t{%d, %q, -1, %d, BookIngredient%s, %d, %d},\n", displayIDOf(recipes, nr.id), "minecraft:"+nr.id,
			r.bookCategory(nr.recipe), goSquare(r.squareOf(nr.recipe.field("ingredient"))), r.itemID(result.str("id")), result.intOr("count", 1))
	}
	f.WriteString(strings.ReplaceAll(recipeBookSmithingDisplays, "{version}", version))
	for _, nr := range recipes {
		kind := nr.recipe.str("type")
		if kind != "minecraft:smithing_transform" && kind != "minecraft:smithing_trim" {
			continue
		}
		var template, addition bookSquare
		if nr.recipe.has("template") {
			template = r.squareOf(nr.recipe.field("template"))
		}
		if nr.recipe.has("addition") {
			addition = r.squareOf(nr.recipe.field("addition"))
		}
		resultID, count, pattern := 0, 0, 0
		trim := kind == "minecraft:smithing_trim"
		if trim {
			var ok bool
			pattern, ok = trimPatterns[nr.recipe.str("pattern")]
			if !ok {
				return fmt.Errorf("unknown trim pattern in %s", nr.id)
			}
		} else {
			result := asObj(nr.recipe.field("result"), "a result")
			if result.has("components") {
				return fmt.Errorf("smithing recipe %s produces a stack with components", nr.id)
			}
			resultID, count = r.itemID(result.str("id")), result.intOr("count", 1)
		}
		f.Printf("\t{%d, %q, -1, %d, BookIngredient%s, BookIngredient%s, BookIngredient%s, %d, %d, %t, %d},\n",
			displayIDOf(recipes, nr.id), "minecraft:"+nr.id, r.bookCategory(nr.recipe), goSquare(template),
			goSquare(r.squareOf(nr.recipe.field("base"))), goSquare(addition), resultID, count, trim, pattern)
	}
	f.WriteString("}\n\n")
	displayCount := 0
	for _, nr := range recipes {
		if hasDisplay(nr.recipe.str("type")) {
			displayCount += recipeDisplayCount(nr.recipe)
		}
	}
	f.Printf("// BuiltinRecipeDisplayCount includes displays whose components the tables cannot represent.\nconst BuiltinRecipeDisplayCount = %d\n", displayCount)

	f.Printf("// Workstation recipe book category IDs from the vanilla registry report.\nconst (RecipeBookCategoryStonecutter = %d; RecipeBookCategorySmithing = %d)\n", r.categories["minecraft:stonecutter"], r.categories["minecraft:smithing"])
	for _, ps := range recipePropertySets {
		e.logf("recipebook: %s holds %d items", ps.name, len(sets[ps.name]))
	}
	e.logf("recipebook: %d stonecutter recipes", len(stonecutter))
	fresh := 0
	for _, d := range displays {
		if d.fresh {
			fresh++
		}
	}
	e.logf("recipebook: %d crafting displays, %d fresh", len(displays), fresh)
	return e.emit("registry/recipebook_gen.go", f)
}

func goSquare(s bookSquare) string {
	switch {
	case s.tag != "":
		return fmt.Sprintf(`{"%s", nil}`, s.tag)
	case len(s.items) > 0:
		return fmt.Sprintf(`{"", []int32%s}`, goInts(s.items))
	}
	return `{"", nil}`
}

func goDisplay(d craftingDisplay) string {
	squares := make([]string, len(d.squares))
	ingredients := make([]string, len(d.squares))
	for i, s := range d.squares {
		squares[i] = goSquare(s)
		ingredients[i] = "nil"
		if len(s.items) > 0 {
			ingredients[i] = goInts(s.items)
		}
	}
	patch := "nil"
	if len(d.resultComponents) > 0 {
		var components []string
		for _, component := range d.resultComponents {
			components = append(components, fmt.Sprintf("{Type: protocol.DataComponent%s, Data: %#v}", component.kind, component.data))
		}
		patch = "[]protocol.RawComponent{" + strings.Join(components, ",") + "}"
	}
	return fmt.Sprintf(`{%d, "%s", %t, %d, %d, %t, %d, %d, []BookIngredient{%s}, []Ingredient{%s}, %d, %d, %t, %s, %s}`,
		d.id, d.name, d.fresh, d.group, d.category, d.shaped, d.width, d.height,
		strings.Join(squares, ", "), strings.Join(ingredients, ", "),
		d.result, d.resultCount, d.notify, goCraftingSlots(d.slots), patch)
}
