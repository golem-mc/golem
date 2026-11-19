package main

import "testing"

func TestTransmuteDisplayCountsAndNumbering(t *testing.T) {
	for _, tc := range []struct {
		bounds string
		count  int
	}{
		{"", 1}, {`,"material_count":3`, 1}, {`,"material_count":{"min":2,"max":5}`, 4}, {`,"material_count":{"min":1}`, 8},
	} {
		recipe := decode([]byte(`{"type":"minecraft:crafting_transmute"`+tc.bounds+`}`), "transmute")
		if n := recipeDisplayCount(recipe); n != tc.count {
			t.Errorf("bounds %s: count=%d, want %d", tc.bounds, n, tc.count)
		}
		recipes := []namedRecipe{{"a", recipe}, {"b", decode([]byte(`{"type":"minecraft:crafting_shapeless"}`), "next")}}
		if id := displayIDOf(recipes, "b"); id != tc.count {
			t.Errorf("next ID=%d, want %d", id, tc.count)
		}
	}
}
