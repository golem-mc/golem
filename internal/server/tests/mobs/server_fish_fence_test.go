package mobs

import (
	"math"
	"testing"

	"github.com/golem-mc/golem/registry"
)

// TestACodSwimsRoundAWaterloggedFence: a glass tank of water, 7x7 and three
// deep, is split by a waterlogged fence with a gap of plain water at its
// south edge. Ordered across, a cod swims through the gap, never into the
// fence: the fence holds water but refuses a swim.
// vanilla: SwimNodeEvaluator.getPathTypeOfMob, FenceBlock.isPathfindable
func TestACodSwimsRoundAWaterloggedFence(t *testing.T) {
	t.Parallel()
	srv, _ := squidTests(t)
	const (
		x0, z0, y0 = 13, -11, 62 // the tank's north-west bottom water cell
		fenceX     = x0 + 3
		gapZ       = z0 + 6
	)
	water, _ := registry.FindState("minecraft:water", map[string]string{"level": "0"})
	glass := registry.BlockGlass.DefaultState()
	fence, ok := registry.WithProps(registry.BlockOakFence.DefaultState(),
		registry.PropValue{P: registry.PropNorth, V: "true"},
		registry.PropValue{P: registry.PropSouth, V: "true"},
		registry.PropValue{P: registry.PropWaterlogged, V: "true"})
	if !ok {
		t.Fatal("no connected waterlogged oak fence state")
	}
	for x := int32(x0 - 1); x <= x0+7; x++ {
		for z := int32(z0 - 1); z <= z0+7; z++ {
			for y := int32(y0 - 1); y <= y0+3; y++ {
				inside := x >= x0 && x < x0+7 && z >= z0 && z < z0+7 && y >= y0 && y < y0+3
				state := water
				switch {
				case !inside && y < y0+3:
					state = glass
				case !inside:
					state = 0
				case x == fenceX && z != gapZ:
					state = fence
				}
				srv.SetBlock(x, y, z, state)
			}
		}
	}
	if got := srv.BlockAt(fenceX, y0, z0); got != fence {
		t.Fatalf("the fence cell holds state %d, want %d: the tank's chunk is not loaded", got, fence)
	}

	id := srv.SpawnMob("minecraft:cod", x0+1.5, y0, z0+0.5)
	srv.TickMobs(1) // a navigation plans only for a body it has seen in water
	tx, ty, tz := float64(x0)+5.5, float64(y0), float64(z0)+0.5
	for tick := range 300 {
		if !srv.SwimMob(id, tx, ty, tz, 1) {
			t.Fatalf("tick %d: the cod was given no path across the tank", tick)
		}
		srv.TickMobs(1)
		m, ok := fishAt(srv, id)
		if !ok {
			t.Fatal("the cod left the world")
		}
		if x, z := int32(math.Floor(m.X)), int32(math.Floor(m.Z)); x == fenceX && z != gapZ {
			t.Fatalf("tick %d: the cod is in the fence at (%.2f, %.2f, %.2f)", tick, m.X, m.Y, m.Z)
		}
		if m.X > fenceX+1.5 {
			return
		}
	}
	m, _ := fishAt(srv, id)
	t.Errorf("the cod ended at (%.2f, %.2f, %.2f), want it across the fence", m.X, m.Y, m.Z)
}
