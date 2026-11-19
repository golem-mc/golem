package rules

import (
	"testing"

	"github.com/golem-mc/golem/registry"
)

// BenchmarkUpdateNeighborShapes measures the neighbour shape pass around a
// placed block whose four sides hold a fence, a wall, a stair and a gate.
func BenchmarkUpdateNeighborShapes(b *testing.B) {
	fence, _ := registry.FindState("minecraft:oak_fence", nil)
	wall, _ := registry.FindState("minecraft:cobblestone_wall", nil)
	stair, _ := registry.FindState("minecraft:oak_stairs", nil)
	gate, _ := registry.FindState("minecraft:oak_fence_gate", nil)
	stone, _ := registry.FindState("minecraft:stone", nil)
	w := newAttachWorld(b)
	const y = 100
	for _, c := range []struct {
		x, z  int32
		state uint16
	}{{0, 0, stone}, {1, 0, fence}, {-1, 0, wall}, {0, 1, stair}, {0, -1, gate}} {
		w.SetBlock(c.x, y, c.z, c.state)
	}
	// Settle the neighbours so the loop measures the no-change path.
	UpdateNeighborShapes(w, 0, y, 0)
	b.ReportAllocs()
	for b.Loop() {
		UpdateNeighborShapes(w, 0, y, 0)
	}
}

// BenchmarkNeighborShapedState measures connection and corner changes from an unsettled state.
func BenchmarkNeighborShapedState(b *testing.B) {
	for _, block := range []registry.Block{registry.BlockOakFence, registry.BlockIronBars, registry.BlockCobblestoneWall, registry.BlockOakFenceGate, registry.BlockOakStairs} {
		b.Run(block.Name(), func(b *testing.B) {
			w := testWorld{
				{-1, 0, 0}: registry.BlockStone.DefaultState(),
				{0, 0, -1}: registry.BlockOakStairs.DefaultState(),
				{1, 0, 0}:  registry.BlockOakStairs.DefaultState(),
				{0, 0, 1}:  registry.BlockCobblestoneWall.DefaultState(),
				{0, 1, 0}:  registry.BlockStone.DefaultState(),
			}
			state, _ := registry.WithProp(block.DefaultState(), registry.PropFacing, "east")
			b.ReportAllocs()
			for b.Loop() {
				NeighborShapedState(w, nil, state, 0, 0, 0)
			}
		})
	}
}
