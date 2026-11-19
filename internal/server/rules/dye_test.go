package rules

import (
	"github.com/golem-mc/golem/internal/protocol"
	"math/rand"
	"testing"

	"github.com/golem-mc/golem/registry"
)

// The dye items and the wool mixing, against the data pack's own
// tables: the sixteen items vanilla's DyeItem answers for, and the pairs
// its crafting mixes and the getMixedColor's dice fall between.

// TestDyeItemColourNamesTheSixteen checks every member of the dyes tag
// names its own colour, and that the items the tag leaves out (a flower's
// dye comes from elsewhere than an item carried in hand) are no dye item.
func TestDyeItemColourNamesTheSixteen(t *testing.T) {
	for _, colour := range []string{
		"white", "orange", "magenta", "light_blue",
		"yellow", "lime", "pink", "gray",
		"light_gray", "cyan", "purple", "blue",
		"brown", "green", "red", "black",
	} {
		id, ok := registry.ItemID("minecraft:" + colour + "_dye")
		if !ok {
			t.Fatalf("the item registry has no %s dye", colour)
		}
		if got, ok := DyeItemColour(protocol.ItemStack{ItemID: id, Count: 1}); !ok || got != colour {
			t.Errorf("the %s dye's colour = %q (found %v), want %q", colour, got, ok, colour)
		}
	}
	for _, name := range []string{"minecraft:wheat", "minecraft:ink_sac", "minecraft:bone_meal"} {
		id, ok := registry.ItemID(name)
		if !ok {
			t.Fatalf("the item registry has no %s", name)
		}
		if _, ok := DyeItemColour(protocol.ItemStack{ItemID: id, Count: 1}); ok {
			t.Errorf("%s answered as a dye item, want the tag's sixteen alone", name)
		}
	}
}

// TestMixedWoolColourFollowsTheDataPacksRecipes pins the mixed colour of
// every pair the data pack carries a two-dye crafting for, both orders of
// one of them, and the same colour with itself, which no recipe mixes and
// the dice answer with that colour either way.
// vanilla: the data pack's *_dye recipes, DyeColor.getMixedColor
func TestMixedWoolColourFollowsTheDataPacksRecipes(t *testing.T) {
	cases := []struct{ a, b, want string }{
		{"blue", "green", "cyan"},
		{"blue", "red", "purple"},
		{"blue", "white", "light_blue"},
		{"black", "white", "gray"},
		{"gray", "white", "light_gray"},
		{"green", "white", "lime"},
		{"purple", "pink", "magenta"},
		{"red", "yellow", "orange"},
		{"red", "white", "pink"},
	}
	rng := rand.New(rand.NewSource(1))
	for _, tc := range cases {
		if got := MixedWoolColour(tc.a, tc.b, rng); got != tc.want {
			t.Errorf("%s mixed with %s = %q, want %q", tc.a, tc.b, got, tc.want)
		}
		if got := MixedWoolColour(tc.b, tc.a, rng); got != tc.want {
			t.Errorf("%s mixed with %s = %q, want the shapeless %q (either order)", tc.b, tc.a, got, tc.want)
		}
	}
	for _, colour := range []string{"white", "blue", "black"} {
		if got := MixedWoolColour(colour, colour, rng); got != colour {
			t.Errorf("%s mixed with itself = %q, want itself", colour, got)
		}
	}
}

// TestMixedWoolColourWithoutARecipeKeepsAParent: a pair the data pack
// mixes no recipe for never invents a third colour; the dice answer only
// one of the parents, and a colour no dye item carries falls to the dice
// with the pair it stands in.
func TestMixedWoolColourWithoutARecipeKeepsAParent(t *testing.T) {
	for s := range 100 {
		rng := rand.New(rand.NewSource(int64(s)))
		for _, pair := range [][2]string{{"blue", "yellow"}, {"black", "brown"}, {"", "red"}} {
			got := MixedWoolColour(pair[0], pair[1], rng)
			if got != pair[0] && got != pair[1] {
				t.Errorf("seed %d: %q and %q mixed into %q, want one of the parents", s, pair[0], pair[1], got)
			}
		}
	}
}
