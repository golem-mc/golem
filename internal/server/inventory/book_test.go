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

// TestRecipeBookOfRenamesOldRecipes checks a book saved before 4769 gets the
// stonecutter recipes' new names, and a current one is left alone.
// vanilla: DataFixers v4769 "Rename stonecutter recipes"
func TestRecipeBookOfRenamesOldRecipes(t *testing.T) {
	t.Parallel()
	old := &RecipeBook{Known: []string{"minecraft:quartz_slab_from_stonecutting"}}
	for version, want := range map[int32]string{
		4700: "minecraft:quartz_slab_from_quartz_block_stonecutting",
		5023: "minecraft:quartz_slab_from_stonecutting",
	} {
		root := nbt.Compound{"DataVersion": nbt.Int(version), "recipeBook": old.Tag(nbt.Compound{})}
		if got := RecipeBookOf(root).Known; len(got) != 1 || got[0] != want {
			t.Errorf("version %d: known = %v, want %s", version, got, want)
		}
	}
}
