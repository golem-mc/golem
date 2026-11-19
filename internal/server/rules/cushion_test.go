package rules

import (
	"testing"

	"github.com/golem-mc/golem/registry"
)

// TestCushionPlacementReadsTheShapesUnderIt checks the placement test on a
// cushion box at y 64 above a few cells at y 63 and 64.
// vanilla: Cushion.canBePlacedAt
func TestCushionPlacementReadsTheShapesUnderIt(t *testing.T) {
	t.Parallel()
	stone := registry.BlockStone.DefaultState()
	slab := registry.MustState("minecraft:stone_slab", map[string]string{"type": "bottom"})
	carpet := registry.BlockWhiteCarpet.DefaultState()
	for _, tc := range []struct {
		name   string
		blocks testWorld
		y      float64
		want   bool
	}{
		{"on stone", testWorld{{0, 63, 0}: stone}, 64, true},
		{"on air", testWorld{}, 64, false},
		{"inside a stone", testWorld{{0, 63, 0}: stone, {0, 64, 0}: stone}, 64, false},
		{"on a carpet's top", testWorld{{0, 64, 0}: carpet}, 64 + 1.0/16, true},
		{"on a slab's top", testWorld{{0, 64, 0}: slab}, 64.5, true},
		{"buried in a slab", testWorld{{0, 63, 0}: stone, {0, 64, 0}: slab}, 64, false},
	} {
		if got := CushionCanBePlacedAt(tc.blocks, 0, tc.y, 0, 1, tc.y+0.25, 1); got != tc.want {
			t.Errorf("%s: CushionCanBePlacedAt = %v, want %v", tc.name, got, tc.want)
		}
	}
}
