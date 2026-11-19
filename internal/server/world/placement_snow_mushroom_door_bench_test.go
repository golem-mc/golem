package world

import (
	"testing"

	"github.com/golem-mc/golem/registry"
)

// BenchmarkSnowMushroomDoorNeighbourShape measures the updates shared with placement.
func BenchmarkSnowMushroomDoorNeighbourShape(b *testing.B) {
	for _, block := range []registry.Block{registry.BlockGrassBlock, registry.BlockBrownMushroomBlock, registry.BlockOakDoor} {
		b.Run(block.Name(), func(b *testing.B) {
			w := newTestWorld(b, NewFlatGenerator())
			b.Cleanup(func() { _ = w.Close() })
			w.Acquire(ChunkPos{})
			p := NewPhysics(w)
			const x, y, z = int32(8), GroundLevel + 8, int32(8)
			state := block.DefaultState()
			neighbor := registry.BlockSnowBlock.DefaultState()
			if block == registry.BlockBrownMushroomBlock {
				neighbor = state
			}
			if block == registry.BlockOakDoor {
				state, _ = registry.WithProp(state, registry.PropHalf, "lower")
				neighbor, _ = registry.WithProp(state, registry.PropHalf, "upper")
				neighbor, _ = registry.WithBool(neighbor, registry.PropOpen, true)
			}
			w.SetBlock(x, y-1, z, registry.BlockStone.DefaultState())
			w.SetBlock(x, y+1, z, neighbor)
			changes := make([]BlockChange, 0, 8)
			b.ReportAllocs()
			for b.Loop() {
				w.SetBlock(x, y, z, state)
				changes = changes[:0]
				if block == registry.BlockOakDoor {
					p.shapeUpdate(x, y, z, [3]int32{0, -1, 0}, &changes)
				} else {
					p.updateAsNeighbor(x, y, z, &changes, false, 0, neighbor, [3]int32{0, 1, 0})
				}
			}
			if len(changes) == 0 || w.BlockAt(x, y, z) == state {
				b.Fatal("shape update did not change the block")
			}
		})
	}
}
