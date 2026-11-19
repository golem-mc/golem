package world

import (
	"sync"
	"testing"

	"github.com/golem-mc/golem/registry"
)

// goldenStateBytes separates block identity from the protocol's numeric ids.
var goldenStateBytes = sync.OnceValue(func() [][]byte {
	states := make([][]byte, registry.StateCount)
	for state := range states {
		states[state] = []byte(stateString(uint16(state)) + "\n")
	}
	return states
})

func TestGoldenStateBytes(t *testing.T) {
	for _, tc := range []struct {
		block string
		props map[string]string
		want  string
	}{
		{"minecraft:air", nil, "minecraft:air\n"},
		{"minecraft:oak_log", map[string]string{"axis": "z"}, "minecraft:oak_log[axis=z]\n"},
		{"minecraft:water", map[string]string{"level": "7"}, "minecraft:water[level=7]\n"},
		{"minecraft:sandstone_slab", map[string]string{"type": "top", "waterlogged": "true"}, "minecraft:sandstone_slab[type=top,waterlogged=true]\n"},
	} {
		state, ok := registry.FindState(tc.block, tc.props)
		if !ok {
			t.Fatal(tc.block)
		}
		if got := string(goldenStateBytes()[state]); got != tc.want {
			t.Errorf("%s: %q, want %q", tc.block, got, tc.want)
		}
	}
}
