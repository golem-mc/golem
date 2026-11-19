package world

import (
	"testing"

	"github.com/google/uuid"

	"github.com/golem-mc/golem/registry"
)

// TestBoneMealNeedsTheTreesMinimumHeight checks the sapling's bone meal
// target rule: it holds only when the cell the tree's base height above the
// sapling is inside the build height.
// vanilla: SaplingBlock.isValidBonemealTarget
func TestBoneMealNeedsTheTreesMinimumHeight(t *testing.T) {
	w, p := newPhysicsWorld(t)
	top := int32(worldMinY + worldHeight - 1)
	for _, c := range []struct {
		name   string
		height int32
	}{
		{"minecraft:oak_sapling", 4},
		{"minecraft:spruce_sapling", 5},
		{"minecraft:birch_sapling", 5},
		{"minecraft:jungle_sapling", 4},
		{"minecraft:acacia_sapling", 5},
		{"minecraft:cherry_sapling", 7},
		{"minecraft:dark_oak_sapling", 0},
		{"minecraft:pale_oak_sapling", 0},
		{"minecraft:poplar_sapling", 7},
		{"minecraft:azalea", 6},
	} {
		for _, dy := range []int32{0, 1} {
			y := top - c.height + 1 - dy // dy 0: one cell too high, 1: the highest that fits
			if c.height == 0 {
				y = top - dy
			}
			state, ok := registry.FindState(c.name, nil)
			if !ok {
				t.Fatalf("no state for %s", c.name)
			}
			// A grower with only mega trees is a target only on its 2x2.
			side := int32(1)
			if c.height == 0 {
				side = 2
			}
			for dx := range side {
				for dz := range side {
					w.SetBlock(5+dx, y-1, dz, mustState("minecraft:dirt", nil))
					setBlock(t, w, p, 5+dx, y, dz, state)
				}
			}
			_, _, _, used, _ := p.BoneMeal(5, y, 0, 1, uuid.Nil)
			want := dy == 1 || c.height == 0
			if used != want {
				t.Errorf("%s at y=%d: bone meal used = %v, want %v", c.name, y, used, want)
			}
		}
	}
}

func mustState(name string, props map[string]string) uint16 {
	s, ok := registry.FindState(name, props)
	if !ok {
		panic("no state " + name)
	}
	return s
}
