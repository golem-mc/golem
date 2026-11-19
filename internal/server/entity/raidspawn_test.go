package entity

import (
	"testing"

	"github.com/golem-mc/golem/registry"
)

func TestRaidSpawnSpotOk(t *testing.T) {
	t.Parallel()
	air := registry.BlockAir.DefaultState()
	for _, tc := range []struct {
		block string
		props map[string]string
		want  bool
	}{
		{"minecraft:snow", map[string]string{"layers": "1"}, true},
		{"minecraft:snow", map[string]string{"layers": "3"}, true},
		{"minecraft:grass_block", nil, true},
		{"minecraft:stone", nil, true},
		{"minecraft:oak_leaves", nil, false},
		{"minecraft:glass", nil, false},
		{"minecraft:oak_fence", nil, false},
		{"minecraft:dirt_path", nil, false},
		{"minecraft:glowstone", nil, false},
		{"minecraft:water", nil, false},
	} {
		below := registry.MustState(tc.block, tc.props)
		got := RaidSpawnSpotOk(below, air, air)
		if got != tc.want {
			t.Errorf("RaidSpawnSpotOk(%s %v, air, air) = %v, want %v", tc.block, tc.props, got, tc.want)
		}
	}
}
