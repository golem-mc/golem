package world

import (
	"testing"

	"github.com/golem-mc/golem/registry"
)

// A pack that adds a huge mushroom block to #minecraft:fences does not make
// worldgen shape passes connect its faces, while real fences still connect.
// The tags are process-wide, so the test is not parallel.
// vanilla: HugeMushroomBlock.updateShape, FenceBlock.updateShape
func TestFencesTagExpansionKeepsWorldgenShapeOnFenceBlocks(t *testing.T) {
	t.Cleanup(func() { registry.ApplyPackTags(nil) })
	registry.ApplyPackTags(registry.PackTags{"minecraft:block": {
		"minecraft:fences": {Entries: []registry.PackTagEntry{{ID: "minecraft:brown_mushroom_block"}}},
	}})
	mushroom, ok := registry.FindState("minecraft:brown_mushroom_block", map[string]string{"east": "false"})
	if !ok {
		t.Fatal("no brown mushroom state")
	}
	stone := registry.BlockStone.DefaultState()

	r := newDecorRegion(newGoldenTerrain(), newChunk(0, 0), worldMinY, worldHeight)
	r.set(4, 80, 4, mushroom)
	r.set(5, 80, 4, stone)
	r.set(8, 80, 8, registry.BlockOakFence.DefaultState())
	r.set(9, 80, 8, stone)
	r.templateShapes([]posKey{{4, 80, 4}, {8, 80, 8}})
	if got := r.get(4, 80, 4); got != mushroom {
		t.Errorf("template pass changed mushroom block to %d, want %d", got, mushroom)
	}
	if !registry.StateBool(r.get(8, 80, 8), registry.PropEast) {
		t.Error("template pass: fence did not connect east")
	}

	tb := &treeBuild{r: r, rng: r.rng}
	if got := tb.shapeUpdate(mushroom, treePos{4, 80, 4}, dirEast); got != mushroom {
		t.Errorf("tree pass changed mushroom block to %d, want %d", got, mushroom)
	}
	fence := registry.BlockOakFence.DefaultState()
	if got := tb.shapeUpdate(fence, treePos{8, 80, 8}, dirEast); !registry.StateBool(got, registry.PropEast) {
		t.Error("tree pass: fence did not connect east")
	}
}
