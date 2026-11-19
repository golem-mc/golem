package registry

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
)

// TestSmithingFitsAbsentIngredientMatchesOnlyEmptySlot checks a recipe's
// ingredient against a slot: an ingredient the recipe leaves out takes only
// an empty slot, and one the tag expansion emptied takes nothing.
// vanilla: Ingredient.testOptionalIngredient
func TestSmithingFitsAbsentIngredientMatchesOnlyEmptySlot(t *testing.T) {
	diamond, ok := ItemID("minecraft:diamond")
	if !ok {
		t.Fatal("the registry has no diamond")
	}
	empty := protocol.ItemStack{}
	held := protocol.ItemStack{ItemID: diamond, Count: 1}

	for _, check := range []struct {
		name       string
		ingredient Ingredient
		stack      protocol.ItemStack
		want       bool
	}{
		{"absent into an empty slot", nil, empty, true},
		{"absent into a filled slot", nil, held, false},
		{"tag-emptied into an empty slot", Ingredient{}, empty, false},
		{"tag-emptied into a filled slot", Ingredient{}, held, false},
		{"listed into its own stack", Ingredient{diamond}, held, true},
		{"listed into another stack",
			Ingredient{diamond + 1}, held, false},
		{"listed into an empty slot", Ingredient{diamond}, empty, false},
	} {
		if got := smithingFits(check.ingredient, check.stack); got != check.want {
			t.Errorf("%s: smithingFits = %t, want %t", check.name, got, check.want)
		}
	}
}
