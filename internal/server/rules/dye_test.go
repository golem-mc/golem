package rules

import (
	"math/rand"
	"testing"
)

// The dye items and the wool mixing, against the data pack's own
// tables: the sixteen items vanilla's DyeItem answers for, and the pairs
// its crafting mixes and the getMixedColor's dice fall between.

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
