package commands

import (
	"slices"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// TestBlockArgumentsSuggestInsideProperties checks that /setblock and /fill
// suggest property names after a `[`, values after an `=`, the separators
// behind a value and a `{` behind a closed list, across spaces inside the
// list, and that /clone's filter does the same for a #tag.
func TestBlockArgumentsSuggestInsideProperties(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false

	_, c := servertest.Join(t, cfg, "Mason", servertest.OpsListing(t, "Mason"))

	cases := []struct {
		line string
		want []string
	}{
		{"/setblock ~ ~ ~ stone_stairs[", []string{"]", "facing=", "half=", "shape=", "waterlogged="}},
		{"/fill ~ ~ ~ ~ ~ ~ oak_stairs[facing=", []string{"east", "north", "south", "west"}},
		{"/setblock ~ ~ ~ oak_stairs[half=top", []string{",", "]"}},
		{"/setblock ~ ~ ~ oak_stairs[ half=top, ", []string{"facing=", "shape=", "waterlogged="}},
		{"/setblock ~ ~ ~ chest[facing=north]", []string{"{"}},
		{"/clone ~ ~ ~ ~ ~ ~ ~ ~ ~ filtered #minecraft:fences[east=", []string{",", "]", "false", "true"}},
	}
	for i, tc := range cases {
		c.CommandSuggestion(int32(i+1), tc.line)
		_, start, length, matches := c.ExpectCommandSuggestions()
		if !slices.Equal(matches, tc.want) {
			t.Errorf("%s: suggestions = %v, want %v", tc.line, matches, tc.want)
		}
		if int(start+length) != len(tc.line) {
			t.Errorf("%s: range ends at %d, want the end of the line %d", tc.line, start+length, len(tc.line))
		}
	}
	c.Disconnect()
}
