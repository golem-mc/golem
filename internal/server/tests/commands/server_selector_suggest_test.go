package commands

import (
	"slices"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// TestSelectorOptionSuggestions pins where a selector option's suggestions
// replace from and what they offer, read off a vanilla server.
func TestSelectorOptionSuggestions(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	_, c, _ := servertest.JoinAs(t, cfg, "Testificate", servertest.OpsListing(t, "Testificate"))

	cases := []struct {
		text    string
		start   int
		length  int
		matches []string
	}{
		{"/kill @e[sort=", 14, 0, []string{"arbitrary", "furthest", "nearest", "random"}},
		{"/kill @e[sort=fu", 14, 2, []string{"furthest"}},
		{"/kill @e[gamemode=", 18, 0, []string{"!adventure", "!creative", "!spectator", "!survival", "adventure", "creative", "spectator", "survival"}},
		{"/kill @e[gamemode=!s", 18, 2, []string{"!spectator", "!survival"}},
		{"/kill @e[gamemode=s", 18, 1, []string{"spectator", "survival"}},
		{"/kill @e[name=", 14, 0, []string{",", "]"}},
		{"/kill @e[limit=", 0, 0, nil},
		{"/tp @p ~", 0, 0, nil},
	}
	for i, tc := range cases {
		id := int32(i + 1)
		c.CommandSuggestion(id, tc.text)
		gotID, start, length, matches := c.ExpectCommandSuggestions()
		if gotID != id || int(start) != tc.start || int(length) != tc.length || !slices.Equal(matches, tc.matches) {
			t.Errorf("%q = start %d length %d %v, want start %d length %d %v",
				tc.text, start, length, matches, tc.start, tc.length, tc.matches)
		}
	}

	c.CommandSuggestion(100, "/kill @e[")
	_, _, _, keys := c.ExpectCommandSuggestions()
	for _, key := range []string{"advancements=", "nbt=", "predicate=", "scores="} {
		if !slices.Contains(keys, key) {
			t.Errorf("/kill @e[ offers %v, missing %s", keys, key)
		}
	}
	c.Disconnect()
}

//go:fix inline
