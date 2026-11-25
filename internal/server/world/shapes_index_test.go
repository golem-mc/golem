package world

import (
	"testing"

	"github.com/golem-mc/golem/registry"
)

func TestShapeBlocksInTracksWrites(t *testing.T) {
	t.Parallel()
	w := newTestWorld(t, NewFlatGenerator())
	w.Acquire(ChunkPos{})
	y := int32(GroundLevel)
	for _, b := range []registry.Block{
		registry.BlockOakFence, registry.BlockCobblestoneWall, registry.BlockOakStairs,
		registry.BlockOakFenceGate, registry.BlockTwistingVines, registry.BlockKelp, registry.BlockPaleMossCarpet,
	} {
		if w.ShapeBlocksIn(0, y, 0, 15, y+1, 15) {
			t.Fatalf("%v: shape block before placing", b)
		}
		w.SetBlock(3, y, 3, b.DefaultState())
		if !w.ShapeBlocksIn(2, y-1, 2, 4, y+1, 4) {
			t.Fatalf("%v: not counted after placing", b)
		}
		w.SetBlock(3, y, 3, registry.BlockAir.DefaultState())
		if w.ShapeBlocksIn(0, y, 0, 15, y+1, 15) {
			t.Fatalf("%v: still counted after removing", b)
		}
	}
}
