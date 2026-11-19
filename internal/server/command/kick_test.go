package command

import "testing"

// TestKickTargetsHasNoSuggestionProvider holds the /kick targets node to
// vanilla's tree, which carries no suggestion provider.
func TestKickTargetsHasNoSuggestionProvider(t *testing.T) {
	targets := Kick.Build().Children[0]
	if targets.Suggests != "" {
		t.Errorf("kick targets suggests = %q, want none", targets.Suggests)
	}
}
