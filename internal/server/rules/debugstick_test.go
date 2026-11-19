package rules

import (
	"testing"

	"github.com/golem-mc/golem/registry"
)

func TestDebugStickSelectWrapsBothWays(t *testing.T) {
	state, _ := registry.FindState("minecraft:oak_stairs", nil)
	names := registry.BlockPropertyNames("minecraft:oak_stairs")
	for _, tc := range []struct {
		current  string
		backward bool
		want     string
	}{
		{"", false, names[0]},
		{"", true, names[len(names)-1]},
		{names[len(names)-1], false, names[0]},
		{names[0], true, names[len(names)-1]},
		{names[1], false, names[2]},
	} {
		got, _, ok := DebugStickSelect(state, tc.current, tc.backward)
		if !ok || got != tc.want {
			t.Errorf("select from %q backward=%v = %q, want %q", tc.current, tc.backward, got, tc.want)
		}
	}
	stone, _ := registry.FindState("minecraft:stone", nil)
	if _, _, ok := DebugStickSelect(stone, "", false); ok {
		t.Error("stone has no property to select")
	}
}
