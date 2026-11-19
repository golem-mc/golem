package command

import (
	"slices"
	"testing"
)

// TestBlockPropertySuggestions checks what a block argument suggests inside
// its property list: the offset the matches replace from, and the matches.
// vanilla: BlockStateParser.fillSuggestions
func TestBlockPropertySuggestions(t *testing.T) {
	setblock := Setblock.Build()
	cases := []struct {
		name  string
		line  string
		start int
		want  []string
	}{
		{"after the bracket", "/setblock ~ ~ ~ minecraft:oak_stairs[", 37,
			[]string{"]", "facing=", "half=", "shape=", "waterlogged="}},
		{"no namespace", "/setblock ~ ~ ~ oak_stairs[", 27,
			[]string{"]", "facing=", "half=", "shape=", "waterlogged="}},
		{"name prefix", "/setblock ~ ~ ~ oak_stairs[fa", 27, []string{"facing="}},
		{"whole name", "/setblock ~ ~ ~ oak_stairs[facing", 33, []string{"="}},
		{"values", "/setblock ~ ~ ~ oak_stairs[facing=", 34, []string{"east", "north", "south", "west"}},
		{"value being typed", "/setblock ~ ~ ~ oak_stairs[half=to", 32, []string{"bottom", "top"}},
		{"after a value", "/setblock ~ ~ ~ oak_stairs[half=top", 35, []string{",", "]"}},
		{"after a comma", "/setblock ~ ~ ~ oak_stairs[half=top,", 36,
			[]string{"facing=", "shape=", "waterlogged="}},
		{"set property left out", "/setblock ~ ~ ~ oak_stairs[half=top,h", 37, nil},
		{"last property needs no comma", "/setblock ~ ~ ~ oak_stairs[facing=north,half=top,shape=straight,waterlogged=true", 80, []string{"]"}},
		{"closed list of a block without a block entity", "/setblock ~ ~ ~ oak_stairs[half=top]", 0, nil},
		{"closed list of a block entity", "/setblock ~ ~ ~ chest[facing=north]", 35, []string{"{"}},
		{"empty list of a block entity", "/setblock ~ ~ ~ chest[]", 23, []string{"{"}},
		{"closed list after a space goes on to the modes", "/setblock ~ ~ ~ chest[facing=north] ", 36,
			[]string{"destroy", "keep", "replace", "strict"}},
		{"closed list that does not hold up", "/setblock ~ ~ ~ chest[facing=up]", 0, nil},
		{"space after the bracket", "/setblock ~ ~ ~ oak_stairs[ ", 28,
			[]string{"]", "facing=", "half=", "shape=", "waterlogged="}},
		{"spaces around a finished entry", "/setblock ~ ~ ~ oak_stairs[ half = top , ", 41,
			[]string{"facing=", "shape=", "waterlogged="}},
		{"name prefix after a space", "/setblock ~ ~ ~ oak_stairs[half=top, fa", 37, []string{"facing="}},
		{"value after a space", "/setblock ~ ~ ~ oak_stairs[facing= no", 35, []string{"east", "north", "south", "west"}},
		{"space after a whole value", "/setblock ~ ~ ~ oak_stairs[half=top ", 36, []string{",", "]"}},
		{"space after a whole name", "/setblock ~ ~ ~ oak_stairs[half ", 32, []string{"="}},
		{"unknown property", "/setblock ~ ~ ~ oak_stairs[nope=", 0, nil},
		{"unknown block", "/setblock ~ ~ ~ nope[", 0, nil},
		{"tags are no setblock argument", "/setblock ~ ~ ~ #minecraft:stairs[", 0, nil},
		{"fill", "/fill ~ ~ ~ ~ ~ ~ oak_stairs[facing=", 36, []string{"east", "north", "south", "west"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tree := setblock
			if len(tc.line) > 5 && tc.line[:5] == "/fill" {
				tree = Fill.Build()
			}
			start, got := Suggest(&Node{Children: []*Node{tree}}, tc.line, nil)
			if tc.want == nil {
				if len(got) != 0 {
					t.Errorf("matches = %v, want none", got)
				}
				return
			}
			if start != tc.start || !slices.Equal(got, tc.want) {
				t.Errorf("Suggest = %d %v, want %d %v", start, got, tc.start, tc.want)
			}
		})
	}
}

// TestBlockPredicatePropertySuggestions checks that a block predicate offers
// the properties of a block like a block state does, and those of every
// block of a tag after a #tag[.
// vanilla: BlockStateParser.suggestVaguePropertyName
func TestBlockPredicatePropertySuggestions(t *testing.T) {
	tree := &Node{Children: []*Node{Clone.Build()}}
	const head = "/clone ~ ~ ~ ~ ~ ~ ~ ~ ~ filtered "
	cases := []struct {
		name  string
		tail  string
		start int
		want  []string
	}{
		{"block", "oak_stairs[hal", 45, []string{"half="}},
		{"tag", "#minecraft:fences[", 52, []string{"]", "east=", "north=", "south=", "waterlogged=", "west="}},
		{"tag value", "#minecraft:fences[east=", 57, []string{",", "]", "false", "true"}},
		{"tag after a space", "#minecraft:fences[ east=true, ", 64, []string{"north=", "south=", "waterlogged=", "west="}},
		{"tag value, nothing left to follow", "#minecraft:wooden_buttons[face=ceiling,facing=north,powered=true", 94, []string{"]", "false"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			start, got := Suggest(tree, head+tc.tail, nil)
			if start != tc.start || !slices.Equal(got, tc.want) {
				t.Errorf("Suggest = %d %v, want %d %v", start, got, tc.start, tc.want)
			}
		})
	}
}
