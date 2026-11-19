package player

import (
	"slices"
	"testing"
)

// TestSetTagsDropsDuplicates checks that a saved player's Tags list reads as
// the set vanilla keeps: a tag written twice is one tag.
// vanilla: Entity's read of Tags
func TestSetTagsDropsDuplicates(t *testing.T) {
	t.Parallel()
	var p Player
	p.SetTags([]string{"a", "a", "b", "a"})
	if got := p.Tags(); !slices.Equal(got, []string{"a", "b"}) {
		t.Errorf("SetTags kept %v, want a and b", got)
	}
}
