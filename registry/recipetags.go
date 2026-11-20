package registry

import "sync/atomic"

// itemTagsPacked is set while the data packs change the item tags, which is
// when the built-in recipes' tag ingredients no longer match the generated
// expansion.
var itemTagsPacked atomic.Bool

// noItem fills an ingredient whose tag collects nothing: it keeps the square
// occupied and matches no stack.
var noItem = Ingredient{-1}

// resolveItemTag returns a resolver from an item tag name to the items it
// collects in the tags in force.
func resolveItemTag() func(tag string) Ingredient {
	byName := itemTagIDs(tagsNow.Load().builtin)
	return func(tag string) Ingredient {
		if ids := byName[tag]; len(ids) > 0 {
			return ids
		}
		return noItem
	}
}

// retagBuiltins re-resolves the tag ingredients of the built-in recipes that
// are still in s against the item tags in force, leaving the generated
// expansion for a recipe whose ID is replaced. The tag names come from the
// recipe book displays, which keep them beside the expanded items.
// vanilla: Ingredient (a HolderSet bound by ReloadableServerResources.updateStaticRegistryTags)
func retagBuiltins(s *recipeSet, replaced map[string]bool) {
	if !itemTagsPacked.Load() {
		return
	}
	resolve := resolveItemTag()
	square := func(b BookIngredient, old Ingredient) Ingredient {
		if b.Tag == "" {
			return old
		}
		return resolve(b.Tag)
	}
	squares := func(id string, n int) []BookIngredient {
		if replaced[id] {
			return nil
		}
		if i, ok := builtinRecipes.displayByName[id]; ok && len(CraftingDisplays[i].Squares) == n {
			return CraftingDisplays[i].Squares
		}
		return nil
	}
	retag := func(id string, in []Ingredient) []Ingredient {
		sq := squares(id, len(in))
		if sq == nil {
			return in
		}
		out := make([]Ingredient, len(in))
		for i := range in {
			out[i] = square(sq[i], in[i])
		}
		return out
	}
	for i := range s.shaped {
		s.shaped[i].Pattern = retag(s.shaped[i].ID, s.shaped[i].Pattern)
	}
	for i := range s.shapeless {
		s.shapeless[i].Ingredients = retag(s.shapeless[i].ID, s.shapeless[i].Ingredients)
	}
	for i := range s.stew {
		s.stew[i].Ingredients = retag(s.stew[i].ID, s.stew[i].Ingredients)
	}
	for i := range s.imbue {
		if sq := squares(s.imbue[i].ID, 9); sq != nil {
			s.imbue[i].Source = square(sq[4], s.imbue[i].Source)
			s.imbue[i].Material = square(sq[0], s.imbue[i].Material)
		}
	}
	for i := range s.displays {
		if d := &s.displays[i]; !replaced[d.Name] {
			d.Items = retag(d.Name, d.Items)
		}
	}
	for kind := range s.cooking {
		for i := range s.cooking[kind] {
			r := &s.cooking[kind][i]
			if j, ok := builtinRecipes.furnaceByName[r.ID]; ok && !replaced[r.ID] {
				r.Ingredient = square(FurnaceDisplays[j].Ingredient, r.Ingredient)
			}
		}
	}
	for i := range s.furnaces {
		if d := &s.furnaces[i]; !replaced[d.Name] {
			d.Matches = square(d.Ingredient, d.Matches)
		}
	}
	slot := func(id string, b BookIngredient, old Ingredient) Ingredient {
		if replaced[id] {
			return old
		}
		return square(b, old)
	}
	display := func(id string) (SmithingDisplay, bool) {
		j, ok := builtinRecipes.smithingByName[id]
		if !ok {
			return SmithingDisplay{}, false
		}
		return SmithingDisplays[j], true
	}
	for i := range s.smithing {
		r := &s.smithing[i]
		if d, ok := display(r.ID); ok {
			r.Template, r.Base, r.Addition = slot(r.ID, d.Template, r.Template), slot(r.ID, d.Base, r.Base), slot(r.ID, d.Addition, r.Addition)
		}
	}
	for i := range s.trim {
		r := &s.trim[i]
		if d, ok := display(r.ID); ok {
			r.Template, r.Base, r.Addition = slot(r.ID, d.Template, r.Template), slot(r.ID, d.Base, r.Base), slot(r.ID, d.Addition, r.Addition)
		}
	}
}
