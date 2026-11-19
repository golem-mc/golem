package entity

import (
	"testing"

	"github.com/golem-mc/golem/registry"
)

// vanilla: LivingEntity.hasLineOfSight, ClipContext.Block.COLLIDER
func TestMobSightClipsPartialShapes(t *testing.T) {
	ladder := registry.MustState("minecraft:ladder", map[string]string{"facing": "south", "waterlogged": "true"})
	slab := registry.MustState("minecraft:stone_slab", map[string]string{"type": "bottom"})
	for _, tc := range []struct {
		name     string
		state    uint16
		from, to [3]float64
		clear    bool
	}{
		{"past ladder", ladder, [3]float64{-2, 64.5, 0.5}, [3]float64{3, 64.5, 0.5}, true},
		{"through ladder", ladder, [3]float64{-2, 64.5, 0.1}, [3]float64{3, 64.5, 0.1}, false},
		{"above slab", slab, [3]float64{-2, 64.75, 0.5}, [3]float64{3, 64.75, 0.5}, true},
		{"through slab", slab, [3]float64{-2, 64.25, 0.5}, [3]float64{3, 64.25, 0.5}, false},
		{"water", registry.BlockStates["minecraft:water"], [3]float64{-2, 64.5, 0.5}, [3]float64{3, 64.5, 0.5}, true},
		{"glass", registry.BlockStates["minecraft:glass"], [3]float64{-2, 64.5, 0.5}, [3]float64{3, 64.5, 0.5}, false},
		{"short corner crossing", registry.BlockStates["minecraft:stone"], [3]float64{-0.01, 64.5, 0.1}, [3]float64{0.1, 64.5, -0.01}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := newTestWorld()
			w.set(0, 64, 0, tc.state)
			for _, reverse := range []bool{false, true} {
				from, to := tc.from, tc.to
				if reverse {
					from, to = to, from
				}
				if got := rayClear(w, from[0], from[1], from[2], to[0], to[1], to[2]); got != tc.clear {
					t.Errorf("rayClear(reverse=%v) = %v, want %v", reverse, got, tc.clear)
				}
				m := NewMob(TypeZombie, 1, from[0], from[1], from[2], 1)
				m.Y -= m.eyeHeight()
				if got := m.seesPoint(w, to[0], to[1], to[2]); got != tc.clear {
					t.Errorf("seesPoint(reverse=%v) = %v, want %v", reverse, got, tc.clear)
				}
			}
		})
	}
}

func TestVisualSightClipsShapesAndSkipsGlass(t *testing.T) {
	w := newTestWorld()
	w.set(0, 64, 0, registry.BlockStates["minecraft:glass"])
	if !rayClearVisual(w, -2, 64.75, 0.5, 3, 64.75, 0.5) {
		t.Fatal("glass stopped visual sight")
	}
	w.set(1, 64, 0, slabState(t))
	if !rayClearVisual(w, -2, 64.75, 0.5, 3, 64.75, 0.5) {
		t.Error("slab stopped visual sight above its shape")
	}
	if rayClearVisual(w, -2, 64.25, 0.5, 3, 64.25, 0.5) {
		t.Error("visual sight passed through the slab's shape")
	}
}
