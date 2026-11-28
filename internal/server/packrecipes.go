package server

import (
	"errors"
	"log/slog"
	"slices"
	"strings"

	"github.com/golem-mc/golem/internal/server/datapack"
	"github.com/golem-mc/golem/registry"
)

// loadPackRecipes reads the enabled packs' recipes; a file vanilla's codec
// refuses is added to bad, and its ID's built-in recipe is dropped.
// vanilla: RecipeManager.prepare
func loadPackRecipes(dirs []string, decoder datapack.ResourceDecoder, skip func(file string, err error), bad *packErrors) (registry.PackRecipes, error) {
	var recipes registry.PackRecipes
	raw, err := datapack.LoadResources(dirs, "recipe", skip)
	if err != nil {
		return recipes, err
	}
	// Sorted so recipes that take the same grid are tried in the same order
	// on every load.
	ids := make([]string, 0, len(raw))
	for id := range raw {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	for _, id := range ids {
		if err := decoder.DecodeRecipe(id, raw[id], &recipes); err != nil {
			if errors.Is(err, datapack.ErrBuiltInRecipeType) {
				slog.Error("Couldn't parse element recipe/" + id + ": " + err.Error())
				continue
			}
			bad.add("recipe", id, err)
			recipes.Dropped = append(recipes.Dropped, id)
		}
	}
	if filters := datapack.LoadFilters(dirs); filters.Any() {
		recipes.Hidden = func(id string) bool {
			ns, path, _ := strings.Cut(id, ":")
			return filters.Builtin(ns, "recipe/"+path+".json")
		}
	}
	return recipes, nil
}

// applyPackRecipes layers pack recipes over the built-ins. The layer is
// process-wide, so a server without pack recipes leaves it alone unless it
// changed it itself.
func (s *Server) applyPackRecipes(packs registry.PackRecipes) {
	s.pluginRecipeMu.Lock()
	s.packRecipes = packs
	s.pluginRecipeMu.Unlock()
	recipes := s.layerPluginRecipes(packs)
	// Item tags re-resolve the built-in recipes, so they count as a layer.
	layered := !recipes.Empty() || s.packTagsApplied
	if !layered && !s.packRecipesApplied {
		return
	}
	s.packRecipesApplied = layered
	registry.ApplyPackRecipes(recipes)
}
