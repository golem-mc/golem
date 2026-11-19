package inventory

import (
	"maps"
	"slices"

	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// RecipeBook is the player's saved recipe book: the four books' open and
// filter toggles, the known recipes by file name, and the unseen ones the
// client highlights until Seen Recipe. A shared book is never edited: changes
// go on a Clone that is swapped in whole.
// vanilla: ServerRecipeBook, over RecipeBook and RecipeBookSettings
type RecipeBook struct {
	Settings      RecipeBookSettings
	Known         []string
	ToBeDisplayed []string
}

// RecipeBookSettings are the open and filter switches, a pair per book in
// packet order: crafting, furnace, blast furnace, smoker.
// vanilla: RecipeBookSettings
type RecipeBookSettings struct {
	Open      [4]bool
	Filtering [4]bool
}

// recipeBookSettingKeys are the file names of each book's open and filter
// switches.
// vanilla: RecipeBookSettings.TypeSettings's codecs
var recipeBookSettingKeys = [4][2]string{
	{"isGuiOpen", "isFilteringCraftable"},
	{"isFurnaceGuiOpen", "isFurnaceFilteringCraftable"},
	{"isBlastingFurnaceGuiOpen", "isBlastingFurnaceFilteringCraftable"},
	{"isSmokerGuiOpen", "isSmokerFilteringCraftable"},
}

// FreshRecipeBook is a new player's book: every recipe unlocked on the first
// tick, highlighted as new.
// vanilla: the recipe-unlock advancements' tick rewards
func FreshRecipeBook() *RecipeBook {
	book := &RecipeBook{}
	displays := registry.CraftingDisplaysNow()
	for i := range displays {
		if displays[i].Fresh {
			book.Known = append(book.Known, displays[i].Name)
			book.ToBeDisplayed = append(book.ToBeDisplayed, displays[i].Name)
		}
	}
	return book
}

// RecipeBookOf is the player's book from their file, or a fresh one when the
// file has none. Unknown recipe names are kept so a missing pack's recipes
// survive until the pack returns.
// vanilla: ServerRecipeBook.loadUntrusted
func RecipeBookOf(root nbt.Compound) *RecipeBook {
	book, ok := nbt.Get[nbt.Compound](root, "recipeBook")
	if !ok {
		return FreshRecipeBook()
	}
	readNames := func(field string) []string {
		list, ok := nbt.Get[nbt.List](book, field)
		if !ok || list.Elem != nbt.TypeString {
			return nil
		}
		var names []string
		for _, item := range list.Items {
			if name, ok := item.(nbt.String); ok {
				names = append(names, string(name))
			}
		}
		return names
	}
	read := &RecipeBook{Known: readNames("recipes"), ToBeDisplayed: readNames("toBeDisplayed")}
	for i := range read.Settings.Open {
		if open, ok := nbt.Get[nbt.Byte](book, recipeBookSettingKeys[i][0]); ok {
			read.Settings.Open[i] = open != 0
		}
		// The filtering flag's key carries its book's own name.
		if filtering, ok := nbt.Get[nbt.Byte](book, recipeBookSettingKeys[i][1]); ok {
			read.Settings.Filtering[i] = filtering != 0
		}
	}
	return read
}

// Clone is a copy of the book, for building the next state of it before
// swapping it in.
func (b *RecipeBook) Clone() *RecipeBook {
	return &RecipeBook{
		Settings:      b.Settings,
		Known:         slices.Clone(b.Known),
		ToBeDisplayed: slices.Clone(b.ToBeDisplayed),
	}
}

// Knows reports whether the recipe is one the book holds.
// vanilla: ServerRecipeBook.contains
func (b *RecipeBook) Knows(name string) bool {
	return slices.Contains(b.Known, name)
}

// MarkSeen clears the recipe's highlight as new.
func (b *RecipeBook) MarkSeen(name string) {
	b.ToBeDisplayed = slices.DeleteFunc(b.ToBeDisplayed, func(n string) bool {
		return n == name
	})
}

// Award returns a copy with names added and highlighted, plus the new ones;
// nil and none when all were held.
// vanilla: ServerRecipeBook.addRecipes
func (b *RecipeBook) Award(names []string) (*RecipeBook, []string) {
	next := b.Clone()
	held := make(map[string]struct{}, len(next.Known)+len(names))
	for _, name := range next.Known {
		held[name] = struct{}{}
	}
	var added []string
	for _, name := range names {
		if _, ok := held[name]; ok {
			continue
		}
		held[name] = struct{}{}
		next.Known = append(next.Known, name)
		next.ToBeDisplayed = append(next.ToBeDisplayed, name)
		added = append(added, name)
	}
	if len(added) == 0 {
		return nil, nil
	}
	return next, added
}

// Revoke returns a copy without names and their highlights, plus the ones
// held; nil and none when none were. Names are indexed once, so revoking the
// whole book is linear.
// vanilla: ServerRecipeBook.removeRecipes
func (b *RecipeBook) Revoke(names []string) (*RecipeBook, []string) {
	held := make(map[string]struct{}, len(b.Known))
	for _, name := range b.Known {
		held[name] = struct{}{}
	}
	drop := make(map[string]struct{}, len(names))
	var removed []string
	for _, name := range names {
		if _, ok := held[name]; !ok {
			continue
		}
		if _, dup := drop[name]; dup {
			continue
		}
		drop[name] = struct{}{}
		removed = append(removed, name)
	}
	if len(removed) == 0 {
		return nil, nil
	}
	next := b.Clone()
	gone := func(n string) bool { _, ok := drop[n]; return ok }
	next.Known = slices.DeleteFunc(next.Known, gone)
	next.ToBeDisplayed = slices.DeleteFunc(next.ToBeDisplayed, gone)
	return next, removed
}

// Tag is the book's file form, laid over the loaded recipeBook compound so
// unknown fields ride through: recipes and unseen ones as vanilla's name
// lists, switches that are on as vanilla's bytes, off ones omitted. root is
// not written to.
// vanilla: ServerPlayer.addAdditionalSaveData's recipeBook
func (b *RecipeBook) Tag(root nbt.Compound) nbt.Compound {
	return b.OrderedTag(root).Entries()
}

// OrderedTag writes RecipeBookSettings before the recipe and highlight lists.
// vanilla: ServerRecipeBook.Packed.CODEC
func (b *RecipeBook) OrderedTag(root nbt.Compound) *nbt.OrderedCompound {
	compound, ok := nbt.Get[nbt.Compound](root, "recipeBook")
	if !ok {
		compound = nbt.Compound{}
	} else {
		compound = maps.Clone(compound)
	}
	out := nbt.NewOrderedCompound(10)
	names := func(list []string) nbt.List {
		items := make([]nbt.Tag, 0, len(list))
		for _, name := range list {
			items = append(items, nbt.String(name))
		}
		return nbt.List{Elem: nbt.TypeString, Items: items}
	}
	for i := range b.Settings.Open {
		if b.Settings.Open[i] {
			out.Set(recipeBookSettingKeys[i][0], nbt.Byte(1))
		}
		delete(compound, recipeBookSettingKeys[i][0])
		if b.Settings.Filtering[i] {
			out.Set(recipeBookSettingKeys[i][1], nbt.Byte(1))
		}
		delete(compound, recipeBookSettingKeys[i][1])
	}
	out.Set("recipes", names(b.Known))
	out.Set("toBeDisplayed", names(b.ToBeDisplayed))
	delete(compound, "recipes")
	delete(compound, "toBeDisplayed")
	for _, key := range nbt.HashOrderedKeys(compound) {
		out.Set(key, compound[key])
	}
	return out
}
