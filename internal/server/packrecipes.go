package server

import (
	"errors"
	"log/slog"
	"slices"

	"github.com/golem-mc/golem/internal/server/datapack"
	"github.com/golem-mc/golem/registry"
)

// loadPackRecipes reads the enabled packs' recipes; a file that fails to
// decode is logged and skipped, and its ID's built-in recipe is dropped.
// vanilla: RecipeManager.prepare
func loadPackRecipes(dirs []string, skip func(file string, err error)) (registry.PackRecipes, error) {
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
		if err := datapack.DecodeRecipe(id, raw[id], &recipes); err != nil {
			slog.Error("Couldn't parse element recipe/" + id + ": " + err.Error())
			if !errors.Is(err, datapack.ErrBuiltInRecipeType) {
				recipes.Dropped = append(recipes.Dropped, id)
			}
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
