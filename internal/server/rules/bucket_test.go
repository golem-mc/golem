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

// TestCanPlaceLiquid checks which blocks take a pour into their own cell.
func TestCanPlaceLiquid(t *testing.T) {
	dry := mustState(t, "minecraft:oak_slab", map[string]string{"type": "bottom", "waterlogged": "false"})
	wet := mustState(t, "minecraft:oak_slab", map[string]string{"type": "bottom", "waterlogged": "true"})
	double := mustState(t, "minecraft:oak_slab", map[string]string{"type": "double", "waterlogged": "false"})
	barrier := mustState(t, "minecraft:barrier", map[string]string{"waterlogged": "false"})
	barrierWet := mustState(t, "minecraft:barrier", map[string]string{"waterlogged": "true"})
	for _, tc := range []struct {
		name               string
		fluid              registry.Block
		state              uint16
		sneaking, creative bool
		want               uint16
		ok                 bool
	}{
		{"water into a slab", registry.BlockWater, dry, false, false, wet, true},
		{"already wet", registry.BlockWater, wet, false, false, wet, true},
		{"sneaking goes over the face", registry.BlockWater, dry, true, false, 0, false},
		{"lava never", registry.BlockLava, dry, false, false, 0, false},
		{"double slab", registry.BlockWater, double, false, false, 0, false},
		{"barrier in survival", registry.BlockWater, barrier, false, false, 0, false},
		{"barrier in creative", registry.BlockWater, barrier, false, true, barrierWet, true},
		{"stone", registry.BlockWater, stateOf(t, "minecraft:stone"), false, false, 0, false},
	} {
		got, ok := CanPlaceLiquid(tc.fluid, tc.state, tc.sneaking, tc.creative)
		if ok != tc.ok || (ok && got != tc.want) {
			t.Errorf("%s: got %d, %v, want %d, %v", tc.name, got, ok, tc.want, tc.ok)
		}
	}
}

// TestBucketScoopsABubbleColumnCell: an empty bucket takes water from either
// column and leaves air.
func TestBucketScoopsABubbleColumnCell(t *testing.T) {
	for _, drag := range []string{"true", "false"} {
		col := mustState(t, "minecraft:bubble_column", map[string]string{"drag": drag})
		if !BucketScoopable(col) {
			t.Errorf("drag=%s: the ray passes through the column", drag)
		}
		if after, fluid, ok := BucketScoopTarget(col); !ok || after != airState || fluid != registry.BlockWater {
			t.Errorf("drag=%s: %d %v %v, want air and water", drag, after, fluid, ok)
		}
	}
}

// TestBucketScoopsPowderSnow: powder snow drains to air under the name of
// its own block.
func TestBucketScoopsPowderSnow(t *testing.T) {
	snow := stateOf(t, "minecraft:powder_snow")
	if after, name, ok := BucketScoopTarget(snow); !ok || after != airState || name != registry.BlockPowderSnow {
		t.Errorf("BucketScoopTarget(powder snow) = %d %v %v, want air and powder snow", after, name, ok)
	}
}
