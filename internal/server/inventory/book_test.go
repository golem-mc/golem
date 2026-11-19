package inventory

import (
	"reflect"
	"slices"
	"testing"

	"github.com/golem-mc/golem/nbt"
)

// TestRecipeBookOfReadsWhatTagWrites checks a saved book reads back as the
// same book, and a file without one gives the fresh book.
func TestRecipeBookOfReadsWhatTagWrites(t *testing.T) {
	t.Parallel()
	book := &RecipeBook{
		Known:         []string{"minecraft:crafting_table", "minecraft:an_old_recipe"},
		ToBeDisplayed: []string{"minecraft:an_old_recipe"},
	}
	book.Settings.Open[2] = true
	book.Settings.Filtering[0] = true
	root := nbt.Compound{"recipeBook": book.Tag(nbt.Compound{})}
	if got := RecipeBookOf(root); !reflect.DeepEqual(got, book) {
		t.Errorf("RecipeBookOf(Tag) = %+v, want %+v", got, book)
	}
	if got := RecipeBookOf(nbt.Compound{}); !reflect.DeepEqual(got, FreshRecipeBook()) {
		t.Errorf("a file without a book read as %+v, want the fresh book", got)
	}
}

// TestRevokeClearsTheHighlightAndKeepsTheRest checks a take drops the recipe
// from both lists, counts a name once however often it is asked for, and
// leaves the book it was called on alone.
func TestRevokeClearsTheHighlightAndKeepsTheRest(t *testing.T) {
	t.Parallel()
	book := &RecipeBook{
		Known:         []string{"a", "b", "c"},
		ToBeDisplayed: []string{"a", "b"},
	}
	next, removed := book.Revoke([]string{"b", "b", "z", "a"})
	if !slices.Equal(removed, []string{"b", "a"}) {
		t.Errorf("removed = %v, want [b a]", removed)
	}
	if !slices.Equal(next.Known, []string{"c"}) || len(next.ToBeDisplayed) != 0 {
		t.Errorf("book = %v / %v, want only c known and nothing highlighted", next.Known, next.ToBeDisplayed)
	}
	if len(book.Known) != 3 || len(book.ToBeDisplayed) != 2 {
		t.Errorf("Revoke changed the book it was called on: %v / %v", book.Known, book.ToBeDisplayed)
	}
	if next, removed := book.Revoke([]string{"z"}); next != nil || removed != nil {
		t.Error("a take of nothing held returned a book")
	}
}

// TestAwardAddsOnlyNewNamesOnce checks a give skips held recipes and repeats.
func TestAwardAddsOnlyNewNamesOnce(t *testing.T) {
	t.Parallel()
	book := &RecipeBook{Known: []string{"a"}}
	next, added := book.Award([]string{"a", "b", "b", "c"})
	if !slices.Equal(added, []string{"b", "c"}) {
		t.Errorf("added = %v, want [b c]", added)
	}
	if !slices.Equal(next.Known, []string{"a", "b", "c"}) || !slices.Equal(next.ToBeDisplayed, []string{"b", "c"}) {
		t.Errorf("book = %v / %v", next.Known, next.ToBeDisplayed)
	}
	if next, _ := book.Award([]string{"a"}); next != nil {
		t.Error("a give of held recipes returned a book")
	}
}
