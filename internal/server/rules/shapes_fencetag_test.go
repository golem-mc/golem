package rules

import (
	"testing"

	"github.com/golem-mc/golem/registry"
)

// A pack that adds a huge mushroom block to #minecraft:fences does not make
// the neighbour pass connect its faces: the update goes through the block's
// class, while a real fence still connects to the stone.
// The tags are process-wide, so the test is not parallel.
// vanilla: HugeMushroomBlock.updateShape, FenceBlock.updateShape
func TestFencesTagExpansionKeepsShapeUpdatesOnFenceBlocks(t *testing.T) {
	t.Cleanup(func() { registry.ApplyPackTags(nil) })
	registry.ApplyPackTags(registry.PackTags{"minecraft:block": {
		"minecraft:fences": {Entries: []registry.PackTagEntry{{ID: "minecraft:brown_mushroom_block"}}},
	}})
	mushroom, ok := registry.FindState("minecraft:brown_mushroom_block", map[string]string{"east": "false"})
	if !ok {
		t.Fatal("no brown mushroom state")
	}
	fence := registry.BlockOakFence.DefaultState()
	w := newAttachWorld(t)
	const y = 100
	w.SetBlock(0, y, 0, mushroom)
	w.SetBlock(5, y, 0, fence)
	w.SetBlock(1, y, 0, registry.BlockStone.DefaultState())
	w.SetBlock(6, y, 0, registry.BlockStone.DefaultState())
	UpdateNeighborShapes(w, 1, y, 0)
	UpdateNeighborShapes(w, 6, y, 0)
	if got := w.BlockAt(0, y, 0); got != mushroom {
		t.Errorf("mushroom block changed to %d, want %d", got, mushroom)
	}
	if got := registry.StateBool(w.BlockAt(5, y, 0), registry.PropEast); !got {
		t.Error("fence did not connect to the stone on its east")
	}
}
