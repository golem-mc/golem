package world

import (
	"testing"

	"github.com/golem-mc/golem/registry"
)

// TestFreezeTopLayerRunsAfterTrees pins the decoration order that lets snow
// settle on a crown: freeze_top_layer (TOP_LAYER_MODIFICATION) is placed after
// every tree feature (VEGETAL_DECORATION), and sees the leaves as the top.
func TestFreezeTopLayerRunsAfterTrees(t *testing.T) {
	t.Parallel()

	g := NewOverworldGenerator(12)
	freeze, lastTree := -1, -1
	for i, f := range g.placed {
		if f.feature.Name == "minecraft:freeze_top_layer" {
			freeze = i
		}
		if _, ok := treeFeature(f.feature.Name, f.feature.Biomes); ok {
			lastTree = i
		}
	}
	if freeze < 0 || lastTree < 0 {
		t.Fatalf("placed features lack freeze_top_layer (%d) or a tree (%d)", freeze, lastTree)
	}
	if freeze < lastTree {
		t.Errorf("freeze_top_layer is placed at %d, before the tree at %d", freeze, lastTree)
	}

	leaf, ok := registry.FindState("minecraft:spruce_leaves", map[string]string{"distance": "1"})
	if !ok {
		t.Fatal("no spruce_leaves state")
	}
	if !snowCanSurviveOn(leaf) {
		t.Errorf("a snow layer cannot stand on spruce leaves")
	}
}

// TestSnowSettlesOnTheColumnTop pins what the top cell of a column must be
// for a snow layer to land above it: a leaf block (full top face) and the
// blocks whose top is not full but which the support-override tag lets carry
// snow (mud, soul sand).
func TestSnowSettlesOnTheColumnTop(t *testing.T) {
	t.Parallel()

	g := NewOverworldGenerator(12)
	cold := func(*decorRegion, int32, int32, int32) registry.BiomeClimate {
		return registry.BiomeClimate{Precipitation: true, Temperature: -1}
	}
	for name, props := range map[string]map[string]string{
		"minecraft:spruce_leaves": {"distance": "1"},
		"minecraft:mud":           nil,
		"minecraft:soul_sand":     nil,
	} {
		r := newDecorRegion(g, newChunk(0, 0), worldMinY, worldHeight)
		const x, y, z = 1, 200, 1
		r.set(x, y, z, mustState(name, props))
		r.placeSnowAndFreeze(cold, [3]int32{})
		if got := r.get(x, y+1, z); got != snowByLayers[1] {
			t.Errorf("above %s holds state %d, want a snow layer (%d)", name, got, snowByLayers[1])
		}
	}
}

// TestSnowyTaigaSugarCaneVanilla pins a seed-20260 column of the vanilla 26.3
// recording where sugar cane stands and no snow layer settles on its grass.
func TestSnowyTaigaSugarCaneVanilla(t *testing.T) {
	t.Parallel()
	g := NewOverworldGenerator(20260)
	c := newChunk(410, -196)
	g.Generate(c)
	const x, z = 6564 & 15, -3130 & 15
	if got := c.BlockAt(x, 62, z); registry.BlockOf(got) != registry.BlockGrassBlock || registry.StateBool(got, registry.PropSnowy) {
		t.Fatalf("under the cane = %d, want grass that is not snowy", got)
	}
	for y := int32(63); y <= 65; y++ {
		if got := registry.BlockOf(c.BlockAt(x, y, z)); got != registry.BlockSugarCane {
			t.Errorf("y=%d: got %v, want sugar cane", y, got)
		}
	}
}
