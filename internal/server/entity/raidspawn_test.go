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

func TestRaidSpawnSpotEmptyCells(t *testing.T) {
	t.Parallel()
	air := registry.BlockAir.DefaultState()
	stone := registry.BlockStone.DefaultState()
	snow := registry.MustState("minecraft:snow", map[string]string{"layers": "1"})
	carpet := registry.BlockWhiteCarpet.DefaultState()
	water := registry.BlockWater.DefaultState()
	fire := registry.BlockFire.DefaultState()
	for _, tc := range []struct {
		name             string
		below, at, above uint16
		want             bool
	}{
		{"snow ignores head collision", snow, air, stone, true},
		{"snow needs air at feet", snow, carpet, air, false},
		{"carpet at feet", stone, carpet, air, true},
		{"head collision", stone, air, stone, false},
		{"water at feet", stone, water, air, false},
		{"water at head", stone, air, water, false},
		{"fire at feet", stone, fire, air, false},
		{"fire at head", stone, air, fire, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := RaidSpawnSpotOk(tc.below, tc.at, tc.above); got != tc.want {
				t.Errorf("RaidSpawnSpotOk = %v, want %v", got, tc.want)
			}
		})
	}
}
