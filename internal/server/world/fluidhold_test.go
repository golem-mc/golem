package world

import (
	"testing"

	"github.com/golem-mc/golem/registry"
)

func TestFluidHoldClassification(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name      string
		props     map[string]string
		container bool
		any       bool
		water     bool
		flow      bool
		lava      bool
	}{
		{name: "air", any: true, water: true, flow: true, lava: true},
		{name: "stone", water: true, flow: true, lava: true},
		{name: "water", any: true, water: true, flow: true, lava: true},
		{name: "lava", any: true, water: true, flow: true, lava: true},
		{name: "oak_stairs", container: true, any: true, water: true},
		{name: "oak_stairs", props: map[string]string{"waterlogged": "true"}, container: true, any: true, water: true},
		{name: "oak_slab", props: map[string]string{"type": "double"}, container: true, any: true, water: true},
		{name: "oak_sign", container: true, any: true, water: true},
		{name: "kelp", container: true, any: true},
		{name: "kelp_plant", container: true, any: true},
		{name: "seagrass", container: true, any: true},
		{name: "tall_seagrass", container: true, any: true},
		{name: "bubble_column", water: true, flow: true, lava: true},
		{name: "oak_door", water: true, flow: true, lava: true},
		{name: "ladder", container: true, any: true, water: true},
		{name: "sugar_cane", water: true, flow: true, lava: true},
		{name: "nether_portal", water: true, flow: true, lava: true},
		{name: "end_portal", water: true, flow: true, lava: true},
		{name: "end_gateway", water: true, flow: true, lava: true},
		{name: "structure_void", water: true, flow: true, lava: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			state := registry.MustState("minecraft:"+tc.name, tc.props)
			got := [5]bool{
				fluidContainer(state), canHoldAny(state),
				canHoldSpecific(state, fluidState{kind: fluidWater, amount: 8}),
				canHoldSpecific(state, fluidState{kind: fluidWater, amount: 7}),
				canHoldSpecific(state, fluidState{kind: fluidLava, amount: 8}),
			}
			want := [5]bool{tc.container, tc.any, tc.water, tc.flow, tc.lava}
			if got != want {
				t.Errorf("container, any, water source, water flow, lava = %v, want %v", got, want)
			}
			if tc.container && canHoldSpecific(state, fluidState{kind: fluidWater, amount: 8, falling: true}) {
				t.Error("container accepts falling water")
			}
		})
	}
	for id := registry.StateCount; id < 1<<16; id++ {
		state := uint16(id)
		if fluidContainer(state) || canHoldAny(state) || !canHoldSpecific(state, fluidState{kind: fluidWater, amount: 8}) {
			t.Fatalf("invalid state %d must reject fluid at the coarse gate and read as a non-container", id)
		}
	}
}
