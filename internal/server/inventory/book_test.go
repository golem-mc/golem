package inventory

import (
	"reflect"
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
