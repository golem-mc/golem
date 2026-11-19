package command

import "testing"

// TestForLevelKeepsATreePerLevel checks the cached trees: a level answers
// the same tree each time, and each level its own, so a player whose level
// changes walks the tree of the new one.
func TestForLevelKeepsATreePerLevel(t *testing.T) {
	for level := 0; level <= PermLevelOwners; level++ {
		first := ForLevel(level)
		if ForLevel(level) != first {
			t.Errorf("ForLevel(%d) built a second tree", level)
		}
	}
	for _, step := range []struct {
		level   int
		hasTime bool
	}{
		{0, false},
		{PermLevelGamemasters, true},
		{0, false},
		{PermLevelOwners, true},
		{PermLevelModerators, false},
	} {
		got := FindLiteralChild(ForLevel(step.level), "time") != nil
		if got != step.hasTime {
			t.Errorf("ForLevel(%d) offers /time = %v, want %v", step.level, got, step.hasTime)
		}
	}
	if FindLiteralChild(ForLevel(PermLevelAdmins), "stop") != nil || FindLiteralChild(ForLevel(PermLevelOwners), "stop") == nil {
		t.Error("/stop is not offered at the owner level alone")
	}
}
