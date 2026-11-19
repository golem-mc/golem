package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestCraftingDisplaysKeepResultComponents(t *testing.T) {
	r := &recipeBookReader{
		items:      map[string]int{"minecraft:bowl": 1, "minecraft:suspicious_stew": 2},
		categories: map[string]int{"minecraft:crafting_misc": 3},
		effects:    map[string]int{"minecraft:night_vision": 15},
	}
	for _, tc := range []struct{ kind, ingredients string }{
		{"shapeless", `"ingredients":["minecraft:bowl"]`},
		{"shaped", `"pattern":["b"],"key":{"b":"minecraft:bowl"}`},
	} {
		recipe := decode([]byte(`{"type":"minecraft:crafting_`+tc.kind+`",`+tc.ingredients+`,"result":{"id":"minecraft:suspicious_stew","components":{"minecraft:suspicious_stew_effects":[{"id":"minecraft:night_vision"}]}}}`), "stew")
		displays, err := r.displays([]namedRecipe{{"stew", recipe}})
		if err != nil || len(displays) != 1 {
			t.Fatalf("%s: displays=%v err=%v", tc.kind, displays, err)
		}
		components := displays[0].resultComponents
		// SuspiciousStewEffects.Entry's absent duration defaults to 160 ticks.
		if len(components) != 1 || components[0].kind != "SuspiciousStew" || !bytes.Equal(components[0].data, []byte{1, 15, 0xa0, 1}) {
			t.Errorf("%s: result components=%v", tc.kind, components)
		}
	}
}

func TestRecipeResultComponentsRejectUnsupportedData(t *testing.T) {
	for _, tc := range []struct{ components, message string }{
		{`{"minecraft:damage":1}`, "unsupported result components"},
		{`{"minecraft:suspicious_stew_effects":[],"minecraft:damage":1}`, "unsupported result components"},
		{`{"minecraft:suspicious_stew_effects":[]}`, "want one stew effect, got 0"},
		{`{"minecraft:suspicious_stew_effects":[{"id":"minecraft:poison"},{"id":"minecraft:wither"}]}`, "want one stew effect, got 2"},
	} {
		result := decode([]byte(`{"components":`+tc.components+`}`), "result")
		_, err := readStewEffect("stew.json", result)
		if err == nil || !strings.Contains(err.Error(), "stew.json: "+tc.message) {
			t.Errorf("components=%s: err=%v, want %s", tc.components, err, tc.message)
		}
	}
}
