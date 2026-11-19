package entity

import (
	"math"
	"testing"

	"github.com/golem-mc/golem/registry"
)

// TestPrimedTNTRestsOnTheBlocksCollisionShape checks that the body's fall is
// clipped against each block's collision shape rather than the full cell.
// vanilla: Entity.move
func TestPrimedTNTRestsOnTheBlocksCollisionShape(t *testing.T) {
	cases := []struct {
		block string
		feet  float64
	}{
		{"minecraft:white_carpet", 63.0625},
		{"minecraft:oak_slab", 63.5},
		{"minecraft:oak_fence", 64.5},
		{"minecraft:stone", 64},
	}
	for _, c := range cases {
		t.Run(c.block, func(t *testing.T) {
			w := newTestWorld()
			w.set(4, 63, 2, registry.MustState(c.block, nil))
			tnt := NewTNT(1, 4.5, 67, 2.5, 0, 0, 0, 1000, 0, 0)
			for range 60 {
				tnt.Step(&w)
			}
			if math.Abs(tnt.Y-c.feet) > 1e-9 {
				t.Errorf("TNT feet on %s = %v, want %v", c.block, tnt.Y, c.feet)
			}
		})
	}
}
