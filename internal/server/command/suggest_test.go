package command

import (
	"slices"
	"testing"
)

// TestResourceSuggestions checks resource matching against vanilla's
// SharedSuggestionProvider.filterResources: the lowercased partial may sit at
// the start of the name or path, or after a `.`, `_` or `/`, and the matches
// come back sorted.
func TestResourceSuggestions(t *testing.T) {
	cases := []struct {
		name       string
		partial    string
		candidates []string
		want       []string
	}{
		{"whole name", "minecraft:sl", []string{"minecraft:speed", "minecraft:slowness", "minecraft:slow_falling"}, []string{"minecraft:slow_falling", "minecraft:slowness"}},
		{"path without the namespace", "sp", []string{"minecraft:speed", "minecraft:slowness"}, []string{"minecraft:speed"}},
		{"after an underscore", "fall", []string{"minecraft:speed", "minecraft:slow_falling"}, []string{"minecraft:slow_falling"}},
		{"after an underscore only", "boost", []string{"minecraft:health_boost", "minecraft:jump_boost", "minecraft:speed"}, []string{"minecraft:health_boost", "minecraft:jump_boost"}},
		{"case folded", "Sp", []string{"minecraft:speed", "minecraft:slowness"}, []string{"minecraft:speed"}},
		{"namespace typed", "mine", []string{"minecraft:speed", "minecraft:slowness"}, []string{"minecraft:slowness", "minecraft:speed"}},
		{"everything, sorted", "", []string{"minecraft:speed", "minecraft:haste", "minecraft:slowness"}, []string{"minecraft:haste", "minecraft:slowness", "minecraft:speed"}},
		{"nothing matches", "zz", []string{"minecraft:speed"}, nil},
		{"name without a namespace", "ore", []string{"ore"}, []string{"ore"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ResourceSuggestions(tc.partial, tc.candidates...)
			if !slices.Equal(got, tc.want) {
				t.Errorf("ResourceSuggestions(%q, ...) = %v, want %v", tc.partial, got, tc.want)
			}
		})
	}
}

// TestSuggestReadsATextWithoutTheSlash pins that the slash is skipped only
// when the text has one: an empty or slash-less text completes from its
// first byte instead of slicing past it.
func TestSuggestReadsATextWithoutTheSlash(t *testing.T) {
	tree := &Node{Children: []*Node{
		{Type: NodeLiteral, Name: "greets", Children: []*Node{
			{Type: NodeLiteral, Name: "world"},
		}},
	}}
	for _, tc := range []struct {
		text      string
		wantStart int
		want      []string
	}{
		{"", 0, []string{"greets"}},
		{"gr", 0, []string{"greets"}},
		{"greets w", 7, []string{"world"}},
		{"/", 1, []string{"greets"}},
		{"/greets w", 8, []string{"world"}},
	} {
		start, matches := Suggest(tree, tc.text, nil)
		if !slices.Equal(matches, tc.want) || (tc.want != nil && start != tc.wantStart) {
			t.Errorf("Suggest(%q) = %d, %v; want %d, %v", tc.text, start, matches, tc.wantStart, tc.want)
		}
	}
}
