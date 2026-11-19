package world

import (
	"testing"

	"github.com/golem-mc/golem/registry"
)

func TestFeatureTemplateRetainsWaterWithoutSpreadingTemplateSources(t *testing.T) {
	world := newGoldenTerrain()
	r := newDecorRegion(world, newChunk(0, 0), worldMinY, worldHeight)
	spike := registry.BlockSulfurSpike.DefaultState()
	spike, _ = registry.WithBool(spike, registry.PropWaterlogged, false)
	water := registry.BlockWater.DefaultState()
	// Two adjacent spikes: one inherits water, the other can fill from it on
	// the second pass. A separate pair only has the template's locked source.
	r.set(4, 80, 4, water)
	flowing, _ := registry.WithInt(water, registry.PropLevel, 1)
	r.set(12, 80, 12, flowing)
	r.placeFeatureTemplate(&PlacedTemplate{Changes: []BlockChange{
		{X: 3, Y: 80, Z: 4, State: spike},
		{X: 4, Y: 80, Z: 4, State: spike},
		{X: 8, Y: 80, Z: 8, State: water},
		{X: 9, Y: 80, Z: 8, State: spike},
		{X: 12, Y: 80, Z: 12, State: spike},
	}})
	for _, p := range [][3]int32{{3, 80, 4}, {4, 80, 4}} {
		if !registry.StateBool(r.get(p[0], p[1], p[2]), registry.PropWaterlogged) {
			t.Errorf("spike at %v did not retain or inherit existing water", p)
		}
	}
	if registry.StateBool(r.get(9, 80, 8), registry.PropWaterlogged) {
		t.Fatal("template water spread into a dry spike")
	}
	if registry.StateBool(r.get(12, 80, 12), registry.PropWaterlogged) {
		t.Fatal("spike retained flowing water")
	}
}

func TestFeatureTemplateLeavesDoubleSlabDryInWater(t *testing.T) {
	r := newDecorRegion(newGoldenTerrain(), newChunk(0, 0), worldMinY, worldHeight)
	double, _ := registry.WithProps(registry.BlockMossyStoneBrickSlab.DefaultState(),
		registry.PropValue{P: registry.PropType, V: "double"}, registry.PropValue{P: registry.PropWaterlogged, V: "false"})
	water := registry.BlockWater.DefaultState()
	// One slab replaces a source, the other could fill from it on the second pass.
	r.set(4, 80, 4, water)
	r.set(6, 80, 4, water)
	r.placeFeatureTemplate(&PlacedTemplate{Changes: []BlockChange{
		{X: 4, Y: 80, Z: 4, State: double},
		{X: 5, Y: 80, Z: 4, State: double},
	}})
	for _, x := range []int32{4, 5} {
		if registry.StateBool(r.get(x, 80, 4), registry.PropWaterlogged) {
			t.Errorf("double slab at x=%d waterlogged", x)
		}
	}
}

func TestTemplatePotentSulfurShape(t *testing.T) {
	for _, tc := range []struct {
		below registry.Block
		water bool
		want  string
	}{
		{registry.BlockStone, false, "dry"},
		{registry.BlockStone, true, "wet"},
		{registry.BlockMagmaBlock, true, "dormant"},
		{registry.BlockLava, true, "continuous"},
	} {
		world := newGoldenTerrain()
		r := newDecorRegion(world, newChunk(0, 0), worldMinY, worldHeight)
		r.set(8, 79, 8, tc.below.DefaultState())
		r.set(8, 80, 8, registry.BlockPotentSulfur.DefaultState())
		if tc.water {
			r.set(8, 81, 8, registry.BlockWater.DefaultState())
		}
		r.templateShape(8, 80, 8)
		if got := registry.StateValue(r.get(8, 80, 8), potentSulfurState); got != tc.want {
			t.Errorf("above water %v, below %s: got %s, want %s", tc.water, tc.below.Name(), got, tc.want)
		}
	}
}
