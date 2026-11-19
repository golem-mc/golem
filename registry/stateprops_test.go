package registry_test

import (
	"maps"
	"testing"

	"github.com/golem-mc/golem/registry"
)

// TestStatePropsMatchesParsedName checks the precomputed property table
// against the properties string of every state ID, and that an ID past the
// palette reads as no properties.
func TestStatePropsMatchesParsedName(t *testing.T) {
	t.Parallel()
	for id := range uint16(0xffff) {
		_, raw, ok := registry.BlockStateName(id)
		got := registry.StateProps(id)
		if !ok {
			if len(got) != 0 {
				t.Fatalf("StateProps(%d) = %v for an ID that is no state, want empty", id, got)
			}
			continue
		}
		if want := registry.ParseStateProps(raw); !maps.Equal(got, want) {
			t.Fatalf("StateProps(%d) = %v, want %v", id, got, want)
		}
	}
}
