package rules

import (
	"testing"

	"github.com/golem-mc/golem/registry"
)

// TestBucketFluidSource checks the source read: water and lava at level 0,
// nothing flowing, nothing else.
func TestBucketFluidSource(t *testing.T) {
	for _, tc := range []struct {
		state uint16
		want  registry.Block
	}{
		{mustState(t, "minecraft:water", map[string]string{"level": "0"}), registry.BlockWater},
		{mustState(t, "minecraft:lava", map[string]string{"level": "0"}), registry.BlockLava},
		{mustState(t, "minecraft:water", map[string]string{"level": "3"}), 0},
		{stateOf(t, "minecraft:stone"), 0},
	} {
		got, ok := BucketFluidSource(tc.state)
		if got != tc.want || ok != (tc.want != 0) {
			t.Errorf("BucketFluidSource(%d) = %v, %v, want %v", tc.state, got, ok, tc.want)
		}
	}
}

// TestBucketScoopTarget checks what the scoop leaves: air for a source, the
// dry state for a waterlogged block, nothing for a dry one.
func TestBucketScoopTarget(t *testing.T) {
	lava := mustState(t, "minecraft:lava", map[string]string{"level": "0"})
	if after, fluid, ok := BucketScoopTarget(lava); !ok || after != airState || fluid != registry.BlockLava {
		t.Errorf("lava source: %d %v %v", after, fluid, ok)
	}
	wet := mustState(t, "minecraft:oak_slab", map[string]string{"type": "bottom", "waterlogged": "true"})
	dry := mustState(t, "minecraft:oak_slab", map[string]string{"type": "bottom", "waterlogged": "false"})
	if after, fluid, ok := BucketScoopTarget(wet); !ok || after != dry || fluid != registry.BlockWater {
		t.Errorf("waterlogged slab: %d %v %v, want %d water", after, fluid, ok, dry)
	}
	if !BucketScoopable(wet) || BucketScoopable(dry) {
		t.Error("the ray stops on the wet slab and not the dry one")
	}
	if _, _, ok := BucketScoopTarget(dry); ok {
		t.Error("a dry slab holds nothing to scoop")
	}
}
