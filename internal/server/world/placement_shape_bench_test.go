package world

import (
	"testing"

	"github.com/golem-mc/golem/registry"
)

// BenchmarkPlantNeighbourShape measures the shape updates shared with command placement.
func BenchmarkPlantNeighbourShape(b *testing.B) {
	for _, tc := range []struct {
		name, block, below, above string
		delta                     [3]int32
		unchanged                 bool
	}{
		{"kelp", "kelp_plant", "stone", "air", [3]int32{0, -1, 0}, false},
		{"kelp_irrelevant_direction", "kelp_plant", "stone", "air", [3]int32{1, 0, 0}, true},
		{"dripstone", "pointed_dripstone", "pointed_dripstone", "stone", [3]int32{0, 1, 0}, false},
		{"chorus", "chorus_plant", "end_stone", "chorus_flower", [3]int32{0, -1, 0}, false},
		{"moss", "pale_hanging_moss", "pale_hanging_moss", "stone", [3]int32{0, 1, 0}, false},
	} {
		b.Run(tc.name, func(b *testing.B) {
			w := newTestWorld(b, NewFlatGenerator())
			b.Cleanup(func() { _ = w.Close() })
			w.Acquire(ChunkPos{})
			p := NewPhysics(w)
			const x, y, z = int32(8), GroundLevel + 8, int32(8)
			w.SetBlock(x, y-1, z, registry.MustState("minecraft:"+tc.below, nil))
			w.SetBlock(x, y+1, z, registry.MustState("minecraft:"+tc.above, nil))
			state := registry.MustState("minecraft:"+tc.block, nil)
			if tc.name == "dripstone" {
				state, _ = registry.WithProp(state, registry.PropVerticalDirection, "down")
				w.SetBlock(x, y-1, z, state)
			}
			changes := make([]BlockChange, 0, 8)
			b.ReportAllocs()
			for b.Loop() {
				w.SetBlock(x, y, z, state)
				changes = changes[:0]
				p.shapeUpdate(x, y, z, tc.delta, &changes)
			}
			if tc.unchanged {
				if len(changes) != 0 || w.BlockAt(x, y, z) != state {
					b.Fatal("irrelevant direction changed the block")
				}
			} else if len(changes) == 0 {
				b.Fatal("shape update did not change the block")
			}
		})
	}
}
