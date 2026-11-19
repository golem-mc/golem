package blocklogic

import (
	"testing"

	"github.com/golem-mc/golem/registry"
)

func TestFluidOf(t *testing.T) {
	for _, tc := range []struct {
		name  string
		props map[string]string
		want  FluidState
	}{
		{"air", nil, FluidState{}},
		{"stone", nil, FluidState{}},
		{"water", map[string]string{"level": "0"}, FluidState{FluidWater, 0, true}},
		{"water", map[string]string{"level": "7"}, FluidState{FluidWater, 7, true}},
		{"water", map[string]string{"level": "8"}, FluidState{FluidWater, 8, true}},
		{"lava", map[string]string{"level": "15"}, FluidState{FluidLava, 15, true}},
		{"oak_stairs", map[string]string{"waterlogged": "false"}, FluidState{}},
		{"oak_stairs", map[string]string{"waterlogged": "true"}, FluidState{FluidWater, 0, false}},
		{"kelp", nil, FluidState{FluidWater, 0, false}},
		{"seagrass", nil, FluidState{FluidWater, 0, false}},
		{"bubble_column", nil, FluidState{FluidWater, 0, false}},
	} {
		s := registry.MustState("minecraft:"+tc.name, tc.props)
		if got := FluidOf(s); got != tc.want {
			t.Errorf("%s %v: got %+v, want %+v", tc.name, tc.props, got, tc.want)
		}
	}
	for id := registry.StateCount; id < 1<<16; id++ {
		if f := FluidOf(uint16(id)); f != (FluidState{}) {
			t.Fatalf("invalid state %d: %+v", id, f)
		}
	}
}

func TestFluidAmount(t *testing.T) {
	for level, want := range []int{8, 7, 6, 5, 4, 3, 2, 1, 8, 8, 8, 8, 8, 8, 8, 8} {
		if got := FluidAmount(level); got != want {
			t.Errorf("level %d: %d, want %d", level, got, want)
		}
	}
}
