package entity

import (
	"math"
	"testing"
)

// vanilla: AbstractMinecart.tick halves the fall after moving through lava.
func TestMinecartLavaHalvesFallAfterMoving(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		startY float64
		lavaY  int32
		want   float64
	}{
		{"entering", 70.8, 69, 5.52},
		{"staying", 70.5, 69, 5.52},
		{"leaving", 70, 70, 11.04},
		{"dry", 71, 65, 11.04},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := newTestWorld()
			w.set(0, tc.lavaY, 0, defaultState(t, "minecraft:lava"))
			m := NewMinecart(1, .5, tc.startY, .5)
			m.VY, m.fallDistance = -1, 10
			m.Step(w, true)
			if math.Abs(m.fallDistance-tc.want) > 1e-9 {
				t.Errorf("fall distance = %g, want %g", m.fallDistance, tc.want)
			}
		})
	}
}

func TestMinecartRailClearsFallDistance(t *testing.T) {
	t.Parallel()
	w := newTestWorld()
	cartGround(t, w, 0, 2, 64)
	m := NewMinecart(1, .5, 64.0625, .5)
	m.fallDistance = 10
	m.Step(w, true)
	if m.fallDistance != 0 || m.LandedFall != 0 {
		t.Errorf("rail retained fall=%g landing=%g, want neither", m.fallDistance, m.LandedFall)
	}
}
