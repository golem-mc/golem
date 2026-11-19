package commands

import (
	"slices"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// TestSuggestionsTakeAnyText pins the suggestions for a text the vanilla
// client never sends but any client can: an empty text and one without the
// slash are completed from their first character, and the range of the
// answer counts UTF-16 units, so the two-byte é before it is one of them.
func TestSuggestionsTakeAnyText(t *testing.T) {
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
		{"", 0, 0, nil},
		{"kil", 0, 3, []string{"kill"}},
		{"/kil", 1, 3, []string{"kill"}},
		{"/execute as é run kil", 18, 3, []string{"kill"}},
	}
	for i, tc := range cases {
		id := int32(i + 1)
		c.CommandSuggestion(id, tc.text)
		gotID, start, length, matches := c.ExpectCommandSuggestions()
		if gotID != id || int(start) != tc.start || int(length) != tc.length ||
			(tc.matches != nil && !slices.Equal(matches, tc.matches)) {
			t.Errorf("%q = start %d length %d %v, want start %d length %d %v",
				tc.text, start, length, matches, tc.start, tc.length, tc.matches)
		}
	}
	c.Disconnect()
}
