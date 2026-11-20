package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/datapack"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/plugin"
	"github.com/golem-mc/golem/registry"
)

// pluginRecipeLayer is what plugins changed in the recipe list: the recipes
// they added, in order, and the built-in or pack recipes they removed. It is
// laid over the packs' recipes by applyPackRecipes, so a reload keeps it.
type pluginRecipeLayer struct {
	added   []plugin.Recipe
	removed []string
}

func (h *pluginHost) Recipes() plugin.Recipes { return pluginRecipeList{h} }

type pluginRecipeList struct{ h *pluginHost }

// Add decodes r through the data pack recipe reader, so a plugin's recipe
// crafts, cooks and shows in the book exactly as the same file in a pack
// does, then gives it to every online player.
func (l pluginRecipeList) Add(r plugin.Recipe) error {
	s := l.h.srv
	key := r.RecipeKey()
	if key.Namespace != l.h.Name() {
		return fmt.Errorf("plugin: recipe %s is not in the namespace %q", key, l.h.Name())
	}
	data, err := recipeJSON(r)
	if err != nil {
		return err
	}
	var probe registry.PackRecipes
	if err := datapack.DecodeRecipe(key.String(), data, &probe); err != nil {
		return fmt.Errorf("plugin: recipe %s: %w", key, err)
	}
	if _, err := resultPatch(r); err != nil {
		return err
	}
	s.pluginRecipeMu.Lock()
	if _, ok := s.pluginRecipeOf(key); ok {
		s.pluginRecipeMu.Unlock()
		return fmt.Errorf("plugin: recipe %s already exists", key)
	}
	s.pluginRecipes.added = append(s.pluginRecipes.added, r)
	s.pluginRecipeMu.Unlock()
	s.reapplyRecipes()
	s.sendRecipeChange([]string{key.String()}, nil)
	return nil
}

// Remove drops a recipe a plugin added, or hides a built-in or pack recipe.
func (l pluginRecipeList) Remove(key plugin.Key) bool {
	s := l.h.srv
	name := key.String()
	s.pluginRecipeMu.Lock()
	if i := slices.IndexFunc(s.pluginRecipes.added, func(r plugin.Recipe) bool { return r.RecipeKey() == key }); i >= 0 {
		s.pluginRecipes.added = slices.Delete(s.pluginRecipes.added, i, i+1)
	} else if _, ok := registryRecipe(name); ok && !slices.Contains(s.pluginRecipes.removed, name) {
		s.pluginRecipes.removed = append(s.pluginRecipes.removed, name)
	} else {
		s.pluginRecipeMu.Unlock()
		return false
	}
	s.pluginRecipeMu.Unlock()
	s.reapplyRecipes()
	s.sendRecipeChange(nil, []string{name})
	return true
}

func (l pluginRecipeList) Get(key plugin.Key) (plugin.Recipe, bool) {
	s := l.h.srv
	s.pluginRecipeMu.Lock()
	defer s.pluginRecipeMu.Unlock()
	return s.pluginRecipeOf(key)
}

func (l pluginRecipeList) All() []plugin.Recipe {
	s := l.h.srv
	s.pluginRecipeMu.Lock()
	defer s.pluginRecipeMu.Unlock()
	return s.allPluginRecipes()
}

// pluginRecipeOf finds a recipe by key among the plugins' and the loaded
// ones; the caller holds pluginRecipeMu.
func (s *Server) pluginRecipeOf(key plugin.Key) (plugin.Recipe, bool) {
	if i := slices.IndexFunc(s.pluginRecipes.added, func(r plugin.Recipe) bool { return r.RecipeKey() == key }); i >= 0 {
		return s.pluginRecipes.added[i], true
	}
	return registryRecipe(key.String())
}

func (s *Server) allPluginRecipes() []plugin.Recipe {
	var out []plugin.Recipe
	seen := map[string]bool{}
	add := func(name string) {
		if seen[name] {
			return
		}
		seen[name] = true
		if r, ok := s.pluginRecipeOf(mustKey(name)); ok {
			out = append(out, r)
		}
	}
	for _, d := range registry.CraftingDisplaysNow() {
		add(d.Name)
	}
	for _, d := range registry.FurnaceDisplaysNow() {
		add(d.Name)
	}
	for _, d := range registry.StonecutterDisplaysNow() {
		add(d.Name)
	}
	for _, d := range registry.SmithingDisplaysNow() {
		add(d.Name)
	}
	return out
}

func mustKey(name string) plugin.Key {
	k, _ := plugin.ParseKey(name)
	return k
}

// reapplyRecipes lays the plugins' recipes over the packs' again.
func (s *Server) reapplyRecipes() {
	s.pluginRecipeMu.Lock()
	packs := s.packRecipes
	s.pluginRecipeMu.Unlock()
	s.applyPackRecipes(packs)
}

// layerPluginRecipes returns the packs' recipes with the plugins' laid over
// them: theirs added, the ones they removed dropped.
func (s *Server) layerPluginRecipes(packs registry.PackRecipes) registry.PackRecipes {
	s.pluginRecipeMu.Lock()
	defer s.pluginRecipeMu.Unlock()
	layer := s.pluginRecipes
	if len(layer.added)+len(layer.removed) == 0 {
		return packs
	}
	out := packs
	out.Dropped = append(slices.Clone(packs.Dropped), layer.removed...)
	out.Crafting = slices.Clone(packs.Crafting)
	out.Stonecutting = slices.Clone(packs.Stonecutting)
	out.SmithingTransform = slices.Clone(packs.SmithingTransform)
	for k := range out.Cooking {
		out.Cooking[k] = slices.Clone(packs.Cooking[k])
	}
	first := len(out.Crafting)
	for _, r := range layer.added {
		data, err := recipeJSON(r)
		if err == nil {
			err = datapack.DecodeRecipe(r.RecipeKey().String(), data, &out)
		}
		if err != nil {
			continue // Add decoded it once already
		}
		if patch, err := resultPatch(r); err == nil && len(patch) > 0 {
			if out.Results == nil {
				out.Results = map[string][]registry.ResultComponent{}
			}
			out.Results[r.RecipeKey().String()] = patch
		}
	}
	// A plugin's crafting recipes are in every new player's book.
	for i := first; i < len(out.Crafting); i++ {
		out.Crafting[i].Fresh = true
	}
	return out
}

// sendRecipeChange tells every online player: the stonecutter's list is
// resent and the book gains or loses the recipes.
func (s *Server) sendRecipeChange(added, removed []string) {
	for _, c := range s.playerClients() {
		_ = c.sendUpdateRecipes()
		if len(removed) > 0 {
			c.TakeRecipes(removed)
		}
		if len(added) > 0 {
			c.GiveRecipes(added)
		}
	}
}

// registryRecipe reads a loaded recipe back as the API's value.
func registryRecipe(name string) (plugin.Recipe, bool) {
	key, err := plugin.ParseKey(name)
	if err != nil {
		return nil, false
	}
	if ds := registry.CraftingDisplaysOfName(name); len(ds) > 0 {
		d := ds[0]
		res := pluginStack(registry.RecipeResult(d.Name, protocol.ItemStack{ItemID: d.ResultID, Count: d.ResultCount}))
		if !d.Shaped {
			r := plugin.ShapelessRecipe{Key: key, Result: res}
			for _, sq := range d.Squares {
				r.Ingredients = append(r.Ingredients, bookIngredient(sq))
			}
			return r, true
		}
		r := plugin.ShapedRecipe{Key: key, Result: res, Keys: map[rune]plugin.Ingredient{}}
		var symbols []registry.BookIngredient
		for y := range int(d.Height) {
			row := make([]rune, d.Width)
			for x := range row {
				sq := d.Squares[y*int(d.Width)+x]
				if sq.Tag == "" && len(sq.Items) == 0 {
					row[x] = ' '
					continue
				}
				i := slices.IndexFunc(symbols, func(o registry.BookIngredient) bool {
					return o.Tag == sq.Tag && slices.Equal(o.Items, sq.Items)
				})
				if i < 0 {
					symbols = append(symbols, sq)
					i = len(symbols) - 1
					r.Keys[rune('A'+i)] = bookIngredient(sq)
				}
				row[x] = rune('A' + i)
			}
			r.Pattern = append(r.Pattern, string(row))
		}
		return r, true
	}
	if d := registry.FurnaceDisplayOfName(name); d != nil {
		kinds := [...]string{"smelting", "blasting", "smoking", "campfire_cooking"}
		return plugin.CookingRecipe{Key: key, Kind: plugin.MinecraftKey(kinds[d.Kind]), Input: bookIngredient(d.Ingredient),
			Result: pluginStack(registry.RecipeResult(d.Name, protocol.ItemStack{ItemID: d.ResultID, Count: d.ResultCount})), Experience: d.Experience, CookTime: int(d.Duration)}, true
	}
	if d := registry.StonecutterDisplayOfName(name); d != nil {
		return plugin.StonecuttingRecipe{Key: key, Input: bookIngredient(d.Ingredient),
			Result: pluginStack(registry.RecipeResult(d.Name, protocol.ItemStack{ItemID: d.ResultID, Count: d.ResultCount}))}, true
	}
	if d := registry.SmithingDisplayOfName(name); d != nil && !d.Trim {
		return plugin.SmithingRecipe{Key: key, Template: bookIngredient(d.Template), Base: bookIngredient(d.Base),
			Addition: bookIngredient(d.Addition), Result: pluginStack(registry.RecipeResult(d.Name, protocol.ItemStack{ItemID: d.ResultID, Count: d.ResultCount}))}, true
	}
	return nil, false
}

func bookIngredient(b registry.BookIngredient) plugin.Ingredient {
	out := plugin.Ingredient{}
	if b.Tag != "" {
		out.Tag = mustKey(b.Tag)
	}
	for _, id := range b.Items {
		out.Items = append(out.Items, registry.Item(id))
	}
	return out
}

// recipeJSON writes r as the data pack file that defines the same recipe.
func recipeJSON(r plugin.Recipe) ([]byte, error) {
	var o map[string]any
	var err error
	switch r := r.(type) {
	case plugin.ShapedRecipe:
		keys := map[string]any{}
		for sym, ing := range r.Keys {
			if keys[string(sym)], err = ingredientJSON(ing); err != nil {
				return nil, err
			}
		}
		o = map[string]any{"type": "minecraft:crafting_shaped", "pattern": r.Pattern, "key": keys, "group": r.Group}
		o["result"], err = resultJSON(r.Result)
	case plugin.ShapelessRecipe:
		var ings []any
		for _, ing := range r.Ingredients {
			v, e := ingredientJSON(ing)
			if e != nil {
				return nil, e
			}
			ings = append(ings, v)
		}
		o = map[string]any{"type": "minecraft:crafting_shapeless", "ingredients": ings, "group": r.Group}
		o["result"], err = resultJSON(r.Result)
	case plugin.CookingRecipe:
		o = map[string]any{"type": r.Kind.String(), "experience": r.Experience, "cookingtime": r.CookTime}
		if o["ingredient"], err = ingredientJSON(r.Input); err == nil {
			o["result"], err = resultJSON(r.Result)
		}
	case plugin.StonecuttingRecipe:
		o = map[string]any{"type": "minecraft:stonecutting"}
		if o["ingredient"], err = ingredientJSON(r.Input); err == nil {
			o["result"], err = resultJSON(r.Result)
		}
	case plugin.SmithingRecipe:
		o = map[string]any{"type": "minecraft:smithing_transform"}
		for name, ing := range map[string]plugin.Ingredient{"template": r.Template, "base": r.Base, "addition": r.Addition} {
			if o[name], err = ingredientJSON(ing); err != nil {
				return nil, err
			}
		}
		o["result"], err = resultJSON(r.Result)
	default:
		return nil, fmt.Errorf("plugin: unsupported recipe type %T", r)
	}
	if err != nil {
		return nil, err
	}
	if g, _ := o["group"].(string); g == "" {
		delete(o, "group")
	}
	return json.Marshal(o)
}

func ingredientJSON(ing plugin.Ingredient) (any, error) {
	if ing.Tag != (plugin.Key{}) {
		return "#" + ing.Tag.String(), nil
	}
	names := make([]string, 0, len(ing.Items))
	for _, it := range ing.Items {
		name, ok := registry.ItemName(int32(it))
		if !ok {
			return nil, errors.New("plugin: recipe ingredient is not an item")
		}
		names = append(names, name)
	}
	if len(names) == 0 {
		return nil, errors.New("plugin: recipe ingredient is empty")
	}
	return names, nil
}

func resultJSON(s plugin.ItemStack) (any, error) {
	st, ok := pluginProtocolStack(s)
	if !ok || st.Empty() {
		return nil, errors.New("plugin: recipe result is not a valid stack")
	}
	name, _ := registry.ItemName(st.ItemID)
	return map[string]any{"id": name, "count": st.Count}, nil
}

// resultPatch is the component patch of r's result, which the recipe JSON
// does not carry: the components the stack holds away from its item's defaults.
func resultPatch(r plugin.Recipe) ([]registry.ResultComponent, error) {
	var result plugin.ItemStack
	switch r := r.(type) {
	case plugin.ShapedRecipe:
		result = r.Result
	case plugin.ShapelessRecipe:
		result = r.Result
	case plugin.CookingRecipe:
		result = r.Result
	case plugin.StonecuttingRecipe:
		result = r.Result
	case plugin.SmithingRecipe:
		result = r.Result
	}
	stack, ok := pluginProtocolStack(result)
	if !ok {
		return nil, errors.New("plugin: recipe result is not a valid stack")
	}
	components := registry.StackComponents(stack)
	var patch []registry.ResultComponent
	var probe protocol.ItemStack
	for _, name := range nbt.HashOrderedKeys(components) {
		if !registry.ReadComponent(&probe, name, components[name]) {
			return nil, fmt.Errorf("plugin: recipe result component %s is not supported", name)
		}
		patch = append(patch, registry.ResultComponent{Name: name, Value: components[name]})
	}
	return patch, nil
}
