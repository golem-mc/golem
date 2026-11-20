package registry

import (
	"cmp"
	"slices"
	"strings"
	"sync/atomic"
)

// Cooking recipe kinds, the index of recipeSet.cooking and of PackRecipes.Cooking.
const (
	CookingSmelting = iota
	CookingBlasting
	CookingSmoking
	CookingCampfire
	cookingKinds
)

// PackCrafting is a crafting_shaped or crafting_shapeless recipe a data pack
// defines. Its CraftingDisplay carries the recipe in book form, Name its ID,
// and leaves ID and Group for ApplyPackRecipes to assign.
type PackCrafting struct {
	CraftingDisplay
	// GroupName is the recipe's group; recipes sharing one share a book group.
	GroupName string
}

// PackCooking is a cooking recipe a data pack defines. Its FurnaceDisplay
// carries the recipe in book form, Name its ID, and leaves ID, Kind and Group
// for ApplyPackRecipes to assign.
type PackCooking struct {
	FurnaceDisplay
	// GroupName is the recipe's group; recipes sharing one share a book group.
	GroupName string
}

// recipe returns the cooking recipe the furnaces match.
func (p *PackCooking) recipe() CookingRecipe {
	return CookingRecipe{ID: p.Name, Ingredient: p.Ingredient.Items, ResultID: p.ResultID, ResultCount: p.ResultCount, Experience: p.Experience, CookTime: p.Duration}
}

// PackRecipes holds the recipes the data packs define, by kind. A recipe
// replaces the built-in one of the same ID, whatever its kind.
type PackRecipes struct {
	Crafting          []PackCrafting
	Cooking           [cookingKinds][]PackCooking
	Stonecutting      []StonecutterRecipe
	SmithingTransform []SmithingTransformRecipe
	SmithingTrim      []SmithingTrimRecipe
	Brewing           []BrewingRecipe
	// Dropped lists the IDs of pack files that do not decode: the file
	// shadows the built-in recipe of that ID, so that recipe goes too.
	Dropped []string
}

// Empty reports whether no pack defines a recipe.
func (p PackRecipes) Empty() bool {
	if len(p.Crafting)+len(p.Stonecutting)+len(p.SmithingTransform)+len(p.SmithingTrim)+len(p.Brewing)+len(p.Dropped) > 0 {
		return false
	}
	for _, c := range p.Cooking {
		if len(c) > 0 {
			return false
		}
	}
	return true
}

// recipeSet is the recipes in force: the built-in tables, or those with the
// data packs' laid over them. It is replaced whole so a craft never sees half
// a reload.
type recipeSet struct {
	shaped            []ShapedRecipe
	shapeless         []ShapelessRecipe
	craftOrder        []craftRef
	stew              []StewRecipe
	imbue             []ImbueRecipe
	cooking           [cookingKinds][]CookingRecipe
	smithing          []SmithingTransformRecipe
	trim              []SmithingTrimRecipe
	brewing           []BrewingRecipe
	stonecutting      []StonecutterRecipe
	displays          []CraftingDisplay
	displayByID       map[int32]int
	displayByName     map[string]int
	furnaces          []FurnaceDisplay
	furnaceByID       map[int32]int
	furnaceByName     map[string]int
	stonecutters      []StonecutterDisplay
	stonecutterByName map[string]int
	stonecutterByID   map[int32]int
	smithingDisplays  []SmithingDisplay
	smithingByName    map[string]int
	smithingByID      map[int32]int
	propertySets      []RecipePropertySet
}

var (
	builtinRecipes = newRecipeSet(&recipeSet{
		shaped:           ShapedRecipes,
		shapeless:        ShapelessRecipes,
		stew:             StewRecipes,
		imbue:            ImbueRecipes,
		cooking:          [cookingKinds][]CookingRecipe{SmeltingRecipes, BlastingRecipes, SmokingRecipes, CampfireCookingRecipes},
		smithing:         SmithingTransformRecipes,
		trim:             SmithingTrimRecipes,
		brewing:          BrewingRecipes,
		stonecutting:     StonecutterRecipes,
		displays:         CraftingDisplays,
		furnaces:         FurnaceDisplays,
		stonecutters:     StonecutterDisplays,
		smithingDisplays: SmithingDisplays,
		propertySets:     RecipePropertySets,
	})
	recipesNow atomic.Pointer[recipeSet]
)

// craftRef names a shaped or shapeless recipe of a recipeSet, in the order
// crafting tries them. cells is the number of non-empty grid cells the
// recipe matches, so a grid holding any other count skips it, and symmetric
// says a shaped recipe's mirror is itself.
// vanilla: ShapedRecipePattern.matches, ShapelessRecipe.matches
type craftRef struct {
	shaped    bool
	symmetric bool
	cells     int8
	index     int
}

// recipes returns the recipes in force.
func recipes() *recipeSet {
	if s := recipesNow.Load(); s != nil {
		return s
	}
	return builtinRecipes
}

func newRecipeSet(s *recipeSet) *recipeSet {
	if s.craftOrder == nil {
		for i := range s.shaped {
			s.craftOrder = append(s.craftOrder, craftRef{shaped: true, index: i})
		}
		for i := range s.shapeless {
			s.craftOrder = append(s.craftOrder, craftRef{index: i})
		}
	}
	for i := range s.craftOrder {
		ref := &s.craftOrder[i]
		if !ref.shaped {
			ref.cells = int8(len(s.shapeless[ref.index].Ingredients))
			continue
		}
		ref.symmetric = symmetric(&s.shaped[ref.index])
		for _, ingredient := range s.shaped[ref.index].Pattern {
			if len(ingredient) > 0 {
				ref.cells++
			}
		}
	}
	s.displayByID = make(map[int32]int, len(s.displays))
	s.displayByName = make(map[string]int, len(s.displays))
	for i := range s.displays {
		s.displayByID[s.displays[i].ID] = i
		if _, exists := s.displayByName[s.displays[i].Name]; !exists {
			s.displayByName[s.displays[i].Name] = i
		}
	}
	s.furnaceByID = make(map[int32]int, len(s.furnaces))
	s.furnaceByName = make(map[string]int, len(s.furnaces))
	for i := range s.furnaces {
		s.furnaceByID[s.furnaces[i].ID] = i
		s.furnaceByName[s.furnaces[i].Name] = i
	}
	s.stonecutterByName = make(map[string]int, len(s.stonecutters))
	s.stonecutterByID = make(map[int32]int, len(s.stonecutters))
	for i := range s.stonecutters {
		s.stonecutterByName[s.stonecutters[i].Name] = i
		s.stonecutterByID[s.stonecutters[i].ID] = i
	}
	s.smithingByName = make(map[string]int, len(s.smithingDisplays))
	s.smithingByID = make(map[int32]int, len(s.smithingDisplays))
	for i := range s.smithingDisplays {
		s.smithingByName[s.smithingDisplays[i].Name] = i
		s.smithingByID[s.smithingDisplays[i].ID] = i
	}
	return s
}

// ApplyPackRecipes layers the data packs' recipes over the built-in ones and
// makes the result what crafting, cooking, smithing, the stonecutter and the
// recipe book use. The built-in recipes' tag ingredients follow the item tags
// in force. An empty set with no item tag changed restores the built-in recipes.
// vanilla: RecipeManager.prepare, RecipeManager.apply
func ApplyPackRecipes(p PackRecipes) {
	if p.Empty() && !itemTagsPacked.Load() {
		recipesNow.Store(nil)
		return
	}
	recipesNow.Store(layerRecipes(p))
}

// layerRecipes drops every built-in recipe whose ID a pack file defines and
// adds the pack's.
func layerRecipes(p PackRecipes) *recipeSet {
	replaced := map[string]bool{}
	for _, id := range p.Dropped {
		replaced[id] = true
	}
	for i := range p.Crafting {
		replaced[p.Crafting[i].Name] = true
	}
	for _, kind := range p.Cooking {
		for i := range kind {
			replaced[kind[i].Name] = true
		}
	}
	for i := range p.Stonecutting {
		replaced[p.Stonecutting[i].ID] = true
	}
	for i := range p.SmithingTransform {
		replaced[p.SmithingTransform[i].ID] = true
	}
	for i := range p.SmithingTrim {
		replaced[p.SmithingTrim[i].ID] = true
	}
	for i := range p.Brewing {
		replaced[p.Brewing[i].ID] = true
	}
	kept := func(id string) bool { return !replaced[id] }

	// One list per kind, sorted as vanilla's recipe map is, so the first
	// recipe that matches is the one it finds.
	// vanilla: RecipeManager.prepare, Identifier.compareTo
	s := &recipeSet{
		stew:  filterRecipes(StewRecipes, func(r StewRecipe) bool { return kept(r.ID) }),
		imbue: filterRecipes(ImbueRecipes, func(r ImbueRecipe) bool { return kept(r.ID) }),
		brewing: mergeRecipes(p.Brewing, BrewingRecipes, kept,
			func(r BrewingRecipe) string { return r.ID }),
		smithing: mergeRecipes(p.SmithingTransform, SmithingTransformRecipes, kept,
			func(r SmithingTransformRecipe) string { return r.ID }),
		trim: mergeRecipes(p.SmithingTrim, SmithingTrimRecipes, kept,
			func(r SmithingTrimRecipe) string { return r.ID }),
		stonecutting: mergeRecipes(p.Stonecutting, StonecutterRecipes, kept,
			func(r StonecutterRecipe) string { return r.ID }),
	}
	for kind, builtin := range builtinRecipes.cooking {
		pack := make([]CookingRecipe, len(p.Cooking[kind]))
		for i := range p.Cooking[kind] {
			pack[i] = p.Cooking[kind][i].recipe()
		}
		s.cooking[kind] = mergeRecipes(pack, builtin, kept, func(r CookingRecipe) string { return r.ID })
	}

	layerDisplays(s, p, replaced)
	for i := range p.Crafting {
		d := &p.Crafting[i]
		if d.Shaped {
			s.shaped = append(s.shaped, ShapedRecipe{ID: d.Name, Width: d.Width, Height: d.Height, Pattern: d.Items, ResultID: d.ResultID, ResultCount: d.ResultCount})
		} else {
			s.shapeless = append(s.shapeless, ShapelessRecipe{ID: d.Name, Ingredients: d.Items, ResultID: d.ResultID, ResultCount: d.ResultCount})
		}
	}
	s.shaped = append(s.shaped, filterRecipes(ShapedRecipes, func(r ShapedRecipe) bool { return kept(r.ID) })...)
	s.shapeless = append(s.shapeless, filterRecipes(ShapelessRecipes, func(r ShapelessRecipe) bool { return kept(r.ID) })...)
	retagBuiltins(s, replaced)
	for i := range s.shaped {
		s.craftOrder = append(s.craftOrder, craftRef{shaped: true, index: i})
	}
	for i := range s.shapeless {
		s.craftOrder = append(s.craftOrder, craftRef{index: i})
	}
	id := func(c craftRef) string {
		if c.shaped {
			return s.shaped[c.index].ID
		}
		return s.shapeless[c.index].ID
	}
	slices.SortStableFunc(s.craftOrder, func(a, b craftRef) int { return compareID(id(a), id(b)) })
	s.propertySets = propertySetsOf(s)
	return newRecipeSet(s)
}

// compareID orders recipe IDs by path, then namespace.
// vanilla: Identifier.compareTo
func compareID(a, b string) int {
	an, ap, _ := strings.Cut(a, ":")
	bn, bp, _ := strings.Cut(b, ":")
	if c := strings.Compare(ap, bp); c != 0 {
		return c
	}
	return strings.Compare(an, bn)
}

// mergeRecipes returns the pack's recipes and the built-in ones kept, sorted
// by ID. Recipes of one ID (a stonecutter recipe per input) keep their order.
func mergeRecipes[T any](pack, builtin []T, kept func(string) bool, id func(T) string) []T {
	out := slices.Concat(pack, filterRecipes(builtin, func(r T) bool { return kept(id(r)) }))
	slices.SortStableFunc(out, func(a, b T) int { return compareID(id(a), id(b)) })
	return out
}

func filterRecipes[T any](in []T, keep func(T) bool) []T {
	out := make([]T, 0, len(in))
	for _, r := range in {
		if keep(r) {
			out = append(out, r)
		}
	}
	return out
}

// layerDisplays builds the book displays: the
// built-in ones the packs do not replace, a pack recipe that replaces a
// built-in one keeping its display ID (a crafting one also the unlock criteria
// of its advancement), and the new ones numbered above every display ID the
// vanilla scan assigns, in alphabetical order.
func layerDisplays(s *recipeSet, p PackRecipes, replaced map[string]bool) {
	// The groups the book draws recipes under. Built-in group IDs stay, and a pack
	// group of a built-in name joins it; any other takes a number above them,
	// one per name.
	next := int32(len(RecipeGroupNames))
	groups := map[string]int32{}
	for id, name := range RecipeGroupNames {
		groups[name] = int32(id)
	}
	groupOf := func(name string) int32 {
		if name == "" {
			return -1
		}
		if _, ok := groups[name]; !ok {
			groups[name] = next
			next++
		}
		return groups[name]
	}

	// Every kind's display shares one ID space, so a new ID must clear the
	// cooking, smithing and stonecutter displays as well.
	nextID := int32(BuiltinRecipeDisplayCount)
	ids := map[string]int32{}
	var added []string
	idOf := func(name string) {
		if i, ok := builtinRecipes.displayByName[name]; ok {
			ids[name] = CraftingDisplays[i].ID
		} else if i, ok := builtinRecipes.furnaceByName[name]; ok {
			ids[name] = FurnaceDisplays[i].ID
		} else if i, ok := builtinRecipes.stonecutterByName[name]; ok {
			ids[name] = StonecutterDisplays[i].ID
		} else if i, ok := builtinRecipes.smithingByName[name]; ok {
			ids[name] = SmithingDisplays[i].ID
		} else if !slices.Contains(added, name) {
			added = append(added, name)
		}
	}
	for i := range p.Crafting {
		idOf(p.Crafting[i].Name)
	}
	for _, kind := range p.Cooking {
		for i := range kind {
			idOf(kind[i].Name)
		}
	}
	for _, r := range p.Stonecutting {
		idOf(r.ID)
	}
	for _, r := range p.SmithingTransform {
		idOf(r.ID)
	}
	for _, r := range p.SmithingTrim {
		idOf(r.ID)
	}
	slices.Sort(added)
	for _, name := range added {
		ids[name] = nextID
		nextID++
	}

	s.displays = filterRecipes(CraftingDisplays, func(d CraftingDisplay) bool { return !replaced[d.Name] })
	for i := range p.Crafting {
		d := p.Crafting[i].CraftingDisplay
		d.ID, d.Group = ids[d.Name], groupOf(p.Crafting[i].GroupName)
		if old, ok := builtinRecipes.displayByName[d.Name]; ok {
			d.Fresh = CraftingDisplays[old].Fresh
		}
		s.displays = append(s.displays, d)
	}
	slices.SortFunc(s.displays, func(a, b CraftingDisplay) int { return cmp.Compare(a.ID, b.ID) })

	s.furnaces = filterRecipes(FurnaceDisplays, func(d FurnaceDisplay) bool { return !replaced[d.Name] })
	for kind := range p.Cooking {
		for i := range p.Cooking[kind] {
			d := p.Cooking[kind][i].FurnaceDisplay
			d.ID, d.Kind, d.Group = ids[d.Name], int32(kind), groupOf(p.Cooking[kind][i].GroupName)
			s.furnaces = append(s.furnaces, d)
		}
	}
	slices.SortFunc(s.furnaces, func(a, b FurnaceDisplay) int { return cmp.Compare(a.ID, b.ID) })

	s.stonecutters = filterRecipes(StonecutterDisplays, func(d StonecutterDisplay) bool { return !replaced[d.Name] })
	// The matcher expands a pack ingredient to one recipe per input item;
	// the book keeps one display with the whole ingredient.
	cuts := map[string]int{}
	for _, r := range p.Stonecutting {
		if i, ok := cuts[r.ID]; ok {
			s.stonecutters[i].Ingredient.Items = append(s.stonecutters[i].Ingredient.Items, r.Input)
			continue
		}
		cuts[r.ID] = len(s.stonecutters)
		s.stonecutters = append(s.stonecutters, StonecutterDisplay{
			ID: ids[r.ID], Name: r.ID, Group: -1, Category: RecipeBookCategoryStonecutter,
			Ingredient: BookIngredient{Items: []int32{r.Input}}, ResultID: r.ResultID, ResultCount: r.ResultCount,
		})
	}
	slices.SortFunc(s.stonecutters, func(a, b StonecutterDisplay) int { return cmp.Compare(a.ID, b.ID) })

	s.smithingDisplays = filterRecipes(SmithingDisplays, func(d SmithingDisplay) bool { return !replaced[d.Name] })
	for _, r := range p.SmithingTransform {
		s.smithingDisplays = append(s.smithingDisplays, SmithingDisplay{
			ID: ids[r.ID], Name: r.ID, Group: -1, Category: RecipeBookCategorySmithing,
			Template: BookIngredient{Items: r.Template}, Base: BookIngredient{Items: r.Base}, Addition: BookIngredient{Items: r.Addition},
			ResultID: r.ResultID, ResultCount: r.ResultCount,
		})
	}
	for _, r := range p.SmithingTrim {
		s.smithingDisplays = append(s.smithingDisplays, SmithingDisplay{
			ID: ids[r.ID], Name: r.ID, Group: -1, Category: RecipeBookCategorySmithing,
			Template: BookIngredient{Items: r.Template}, Base: BookIngredient{Items: r.Base}, Addition: BookIngredient{Items: r.Addition},
			Trim: true, Pattern: r.Pattern,
		})
	}
	slices.SortFunc(s.smithingDisplays, func(a, b SmithingDisplay) int { return cmp.Compare(a.ID, b.ID) })
}

// propertySetsOf builds the recipe book's item groups from the recipes of
// each kind: the items the furnaces and the campfire take, the smithing
// slots, and the brewing stand's bottles and reagents. The names and their order are the built-in table's.
// vanilla: RecipeManager.finalizeRecipeLoading
func propertySetsOf(s *recipeSet) []RecipePropertySet {
	items := map[string]map[int32]bool{}
	add := func(set string, ingredient Ingredient) {
		if items[set] == nil {
			items[set] = map[int32]bool{}
		}
		for _, id := range ingredient {
			items[set][id] = true
		}
	}
	for kind, name := range [cookingKinds]string{"furnace_input", "blast_furnace_input", "smoker_input", "campfire_input"} {
		for _, r := range s.cooking[kind] {
			add("minecraft:"+name, r.Ingredient)
		}
	}
	for _, r := range s.smithing {
		add("minecraft:smithing_template", r.Template)
		add("minecraft:smithing_base", r.Base)
		add("minecraft:smithing_addition", r.Addition)
	}
	for _, r := range s.trim {
		add("minecraft:smithing_template", r.Template)
		add("minecraft:smithing_base", r.Base)
		add("minecraft:smithing_addition", r.Addition)
	}
	for _, r := range s.brewing {
		add("minecraft:brewing_input", r.Input)
		add("minecraft:brewing_reagent", r.Reagent)
	}
	out := make([]RecipePropertySet, len(RecipePropertySets))
	for i, set := range RecipePropertySets {
		ids := make([]int32, 0, len(items[set.Name]))
		for id := range items[set.Name] {
			ids = append(ids, id)
		}
		slices.Sort(ids)
		out[i] = RecipePropertySet{Name: set.Name, Items: ids}
	}
	return out
}

// RecipePropertySetsNow returns the recipe book's item groups for the recipes
// in force.
func RecipePropertySetsNow() []RecipePropertySet { return recipes().propertySets }

// StonecutterRecipesNow returns the stonecutter's recipes in the order the
// Update Recipes packet lists them.
func StonecutterRecipesNow() []StonecutterRecipe { return recipes().stonecutting }

// CraftingDisplaysNow returns every crafting recipe the book can show.
func CraftingDisplaysNow() []CraftingDisplay { return recipes().displays }

// FurnaceDisplaysNow returns every cooking recipe the book can show.
func FurnaceDisplaysNow() []FurnaceDisplay { return recipes().furnaces }

// StonecutterDisplaysNow returns every stonecutting recipe the book can show.
func StonecutterDisplaysNow() []StonecutterDisplay { return recipes().stonecutters }

// SmithingDisplaysNow returns every smithing recipe the book can show.
func SmithingDisplaysNow() []SmithingDisplay { return recipes().smithingDisplays }
