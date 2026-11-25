package registry

import (
	"cmp"
	"slices"
	"strings"
	"sync/atomic"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/nbt"
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

// PackDisplays are the book displays of a pack recipe that rewrites a stack,
// a transmute recipe having one per material count. Their ID and Group are for
// ApplyPackRecipes to assign, and GroupName is the recipe's group.
type PackDisplays struct {
	Displays  []CraftingDisplay
	GroupName string
}

// PackTransmute is a crafting_transmute recipe a data pack defines.
type PackTransmute struct {
	TransmuteRecipe
	PackDisplays
}

// PackDye is a crafting_dye recipe a data pack defines.
type PackDye struct {
	DyeRecipe
	PackDisplays
}

// PackImbue is a crafting_imbue recipe a data pack defines.
type PackImbue struct {
	ImbueRecipe
	PackDisplays
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
	Transmute         []PackTransmute
	Dye               []PackDye
	Imbue             []PackImbue
	Cooking           [cookingKinds][]PackCooking
	Stonecutting      []StonecutterRecipe
	SmithingTransform []SmithingTransformRecipe
	SmithingTrim      []SmithingTrimRecipe
	Brewing           []BrewingRecipe
	// Results holds, by recipe ID, the component patch of each recipe whose
	// result carries one.
	// vanilla: ItemStackTemplate.CODEC
	Results map[string][]ResultComponent
	// Dropped lists the IDs of pack files that do not decode: the file
	// shadows the built-in recipe of that ID, so that recipe goes too.
	Dropped []string
	// Hidden reports a built-in recipe ID a pack's filter hides; nil hides none.
	Hidden func(id string) bool
}

// Empty reports whether no pack defines a recipe.
func (p PackRecipes) Empty() bool {
	if p.Hidden != nil || len(p.Crafting)+len(p.Transmute)+len(p.Dye)+len(p.Imbue)+len(p.Stonecutting)+len(p.SmithingTransform)+len(p.SmithingTrim)+len(p.Brewing)+len(p.Dropped) > 0 {
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
	transmute         []TransmuteRecipe
	dye               []DyeRecipe
	cooking           [cookingKinds][]CookingRecipe
	cookingByItem     [cookingKinds][]int32          // first recipe index per item, -1 for none
	cookingByID       [cookingKinds]map[string]int32 // first recipe index per id
	cookingOverlap    [cookingKinds]map[string]int32 // byID restricted to recipes that are a later match for some item
	smithing          []SmithingTransformRecipe
	trim              []SmithingTrimRecipe
	brewing           []BrewingRecipe
	stonecutting      []StonecutterRecipe
	results           map[string][]ResultComponent
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
		transmute:        TransmuteRecipes,
		dye:              DyeRecipes,
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
	// builtinDisplays is how many crafting displays each built-in recipe has.
	builtinDisplays = func() map[string]int32 {
		n := map[string]int32{}
		for i := range CraftingDisplays {
			n[CraftingDisplays[i].Name]++
		}
		return n
	}()
)

// craftKind is the list of a recipeSet a craftRef indexes.
type craftKind uint8

const (
	craftShapeless craftKind = iota
	craftShaped
	craftTransmute
	craftDye
	craftImbue
)

// craftRef names a crafting recipe of a recipeSet, in the order crafting
// tries them. cells is the number of non-empty grid cells the recipe matches,
// so a grid holding any other count skips it; 0 leaves the count to the
// recipe. symmetric says a shaped recipe's mirror is itself.
// vanilla: ShapedRecipePattern.matches, ShapelessRecipe.matches
type craftRef struct {
	kind      craftKind
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
		s.craftOrder = craftRefs(s)
	}
	for i := range s.craftOrder {
		ref := &s.craftOrder[i]
		switch ref.kind {
		case craftShapeless:
			ref.cells = int8(len(s.shapeless[ref.index].Ingredients))
		case craftShaped:
			ref.symmetric = symmetric(&s.shaped[ref.index])
			for _, ingredient := range s.shaped[ref.index].Pattern {
				if len(ingredient) > 0 {
					ref.cells++
				}
			}
		}
	}
	for k := range s.cooking {
		idx := make([]int32, ItemCount)
		for i := range idx {
			idx[i] = -1
		}
		byID := make(map[string]int32, len(s.cooking[k]))
		s.cookingByID[k] = byID
		later := make(map[int32]bool)
		for r := range s.cooking[k] {
			if _, dup := byID[s.cooking[k][r].ID]; !dup {
				byID[s.cooking[k][r].ID] = int32(r)
			}
			for _, id := range s.cooking[k][r].Ingredient {
				if id < 0 || int(id) >= len(idx) {
					continue
				}
				if idx[id] < 0 {
					idx[id] = int32(r)
				} else if idx[id] != int32(r) {
					later[int32(r)] = true
				}
			}
		}
		overlap := make(map[string]int32)
		for id, r := range byID {
			if later[r] {
				overlap[id] = r
			}
		}
		s.cookingOverlap[k] = overlap
		s.cookingByItem[k] = idx
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
	if p.Empty() && !CurrentTagLookup().itemTagsPacked {
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
	for i := range p.Transmute {
		replaced[p.Transmute[i].ID] = true
	}
	for i := range p.Dye {
		replaced[p.Dye[i].ID] = true
	}
	for i := range p.Imbue {
		replaced[p.Imbue[i].ID] = true
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
	if p.Hidden != nil {
		for _, id := range builtinRecipeIDs() {
			if p.Hidden(id) {
				replaced[id] = true
			}
		}
	}
	kept := func(id string) bool { return !replaced[id] }

	// One list per kind, sorted as vanilla's recipe map is, so the first
	// recipe that matches is the one it finds.
	// vanilla: RecipeManager.prepare, Identifier.compareTo
	s := &recipeSet{
		results:   p.Results,
		stew:      filterRecipes(StewRecipes, func(r StewRecipe) bool { return kept(r.ID) }),
		imbue:     filterRecipes(ImbueRecipes, func(r ImbueRecipe) bool { return kept(r.ID) }),
		transmute: filterRecipes(TransmuteRecipes, func(r TransmuteRecipe) bool { return kept(r.ID) }),
		dye:       filterRecipes(DyeRecipes, func(r DyeRecipe) bool { return kept(r.ID) }),
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

	for i := range p.Transmute {
		s.transmute = append(s.transmute, p.Transmute[i].TransmuteRecipe)
	}
	for i := range p.Dye {
		s.dye = append(s.dye, p.Dye[i].DyeRecipe)
	}
	for i := range p.Imbue {
		s.imbue = append(s.imbue, p.Imbue[i].ImbueRecipe)
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
	s.craftOrder = craftRefs(s)
	s.propertySets = propertySetsOf(s)
	return newRecipeSet(s)
}

// builtinRecipeIDs lists the ID of every built-in recipe: the matcher's and
// every display's, which also names the recipes computed from the grid.
func builtinRecipeIDs() []string {
	var ids []string
	for _, d := range CraftingDisplays {
		ids = append(ids, d.Name)
	}
	for _, d := range FurnaceDisplays {
		ids = append(ids, d.Name)
	}
	for _, d := range StonecutterDisplays {
		ids = append(ids, d.Name)
	}
	for _, d := range SmithingDisplays {
		ids = append(ids, d.Name)
	}
	for _, r := range builtinRecipes.shaped {
		ids = append(ids, r.ID)
	}
	for _, r := range builtinRecipes.shapeless {
		ids = append(ids, r.ID)
	}
	for _, r := range builtinRecipes.stew {
		ids = append(ids, r.ID)
	}
	for _, r := range builtinRecipes.transmute {
		ids = append(ids, r.ID)
	}
	for _, r := range builtinRecipes.dye {
		ids = append(ids, r.ID)
	}
	for _, r := range builtinRecipes.imbue {
		ids = append(ids, r.ID)
	}
	for _, kind := range builtinRecipes.cooking {
		for _, r := range kind {
			ids = append(ids, r.ID)
		}
	}
	for _, r := range builtinRecipes.smithing {
		ids = append(ids, r.ID)
	}
	for _, r := range builtinRecipes.trim {
		ids = append(ids, r.ID)
	}
	for _, r := range builtinRecipes.brewing {
		ids = append(ids, r.ID)
	}
	for _, r := range builtinRecipes.stonecutting {
		ids = append(ids, r.ID)
	}
	return ids
}

// craftRefs lists the crafting recipes of s sorted by ID, as vanilla's recipe
// map is, so the first recipe that matches is the one it finds.
// vanilla: RecipeManager.prepare, Identifier.compareTo
func craftRefs(s *recipeSet) []craftRef {
	var refs []craftRef
	var ids []string
	add := func(kind craftKind, n int, id func(int) string) {
		for i := range n {
			refs = append(refs, craftRef{kind: kind, index: i})
			ids = append(ids, id(i))
		}
	}
	add(craftShaped, len(s.shaped), func(i int) string { return s.shaped[i].ID })
	add(craftShapeless, len(s.shapeless), func(i int) string { return s.shapeless[i].ID })
	add(craftTransmute, len(s.transmute), func(i int) string { return s.transmute[i].ID })
	add(craftDye, len(s.dye), func(i int) string { return s.dye[i].ID })
	add(craftImbue, len(s.imbue), func(i int) string { return s.imbue[i].ID })
	order := make([]int, len(refs))
	for i := range order {
		order[i] = i
	}
	slices.SortStableFunc(order, func(a, b int) int { return compareID(ids[a], ids[b]) })
	sorted := make([]craftRef, len(refs))
	for i, o := range order {
		sorted[i] = refs[o]
	}
	return sorted
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
	// sizes is how many displays, so display IDs, a recipe takes.
	sizes := map[string]int32{}
	idOf := func(name string, size int) {
		sizes[name] = int32(size)
		if i, ok := builtinRecipes.displayByName[name]; ok {
			if builtinDisplays[name] >= int32(size) {
				ids[name] = CraftingDisplays[i].ID
				return
			}
		} else if i, ok := builtinRecipes.furnaceByName[name]; ok {
			ids[name] = FurnaceDisplays[i].ID
			return
		} else if i, ok := builtinRecipes.stonecutterByName[name]; ok {
			ids[name] = StonecutterDisplays[i].ID
			return
		} else if i, ok := builtinRecipes.smithingByName[name]; ok {
			ids[name] = SmithingDisplays[i].ID
			return
		}
		if !slices.Contains(added, name) {
			added = append(added, name)
		}
	}
	for i := range p.Crafting {
		idOf(p.Crafting[i].Name, 1)
	}
	for i := range p.Transmute {
		idOf(p.Transmute[i].ID, len(p.Transmute[i].Displays))
	}
	for i := range p.Dye {
		idOf(p.Dye[i].ID, len(p.Dye[i].Displays))
	}
	for i := range p.Imbue {
		idOf(p.Imbue[i].ID, len(p.Imbue[i].Displays))
	}
	for _, kind := range p.Cooking {
		for i := range kind {
			idOf(kind[i].Name, 1)
		}
	}
	for _, r := range p.Stonecutting {
		idOf(r.ID, 1)
	}
	for _, r := range p.SmithingTransform {
		idOf(r.ID, 1)
	}
	for _, r := range p.SmithingTrim {
		idOf(r.ID, 1)
	}
	slices.Sort(added)
	for _, name := range added {
		ids[name] = nextID
		nextID += sizes[name]
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
	packed := func(name, group string, displays []CraftingDisplay) {
		for n, d := range displays {
			d.ID, d.Group = ids[name]+int32(n), groupOf(group)
			if old, ok := builtinRecipes.displayByName[name]; ok && n == 0 {
				d.Fresh = CraftingDisplays[old].Fresh
			}
			s.displays = append(s.displays, d)
		}
	}
	for i := range p.Transmute {
		packed(p.Transmute[i].ID, p.Transmute[i].GroupName, p.Transmute[i].Displays)
	}
	for i := range p.Dye {
		packed(p.Dye[i].ID, p.Dye[i].GroupName, p.Dye[i].Displays)
	}
	for i := range p.Imbue {
		packed(p.Imbue[i].ID, p.Imbue[i].GroupName, p.Imbue[i].Displays)
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

// ResultComponent is one entry of a recipe result's component patch: a
// component name (a "!" prefix removes it) and its value in the codec's NBT shape.
type ResultComponent struct {
	Name  string
	Value nbt.Tag
}

// ApplyResultPatch lays a recipe result's component patch on a stack, false
// when a component is refused.
// vanilla: ItemStackTemplate.apply
func ApplyResultPatch(stack *protocol.ItemStack, patch []ResultComponent) bool {
	for _, c := range patch {
		if !ReadComponent(stack, c.Name, c.Value) {
			return false
		}
	}
	return true
}

// RecipeResult returns the recipe's result: the stack with the patch a data
// pack gave the recipe laid over its components.
func RecipeResult(id string, stack protocol.ItemStack) protocol.ItemStack {
	patch := recipes().results[id]
	if patch == nil {
		return stack
	}
	out := stack
	ApplyResultPatch(&out, patch)
	return out
}

// CookingResult is what one cook of the recipe produces.
func CookingResult(r *CookingRecipe) protocol.ItemStack {
	return RecipeResult(r.ID, protocol.ItemStack{ItemID: r.ResultID, Count: r.ResultCount})
}
