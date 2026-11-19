package world

import (
	"testing"

	"github.com/golem-mc/golem/registry"
)

// BenchmarkWaterloggedStalactiteFall resets and drops a four-block column;
// fluid ticks stay pending so only the column's fall and neighbour wakes run.
func BenchmarkWaterloggedStalactiteFall(b *testing.B) {
	w := newTestWorld(b, NewFlatGenerator())
	b.Cleanup(func() { _ = w.Close() })
	w.Acquire(ChunkPos{})
	p := NewPhysics(w)
	const x, top, z = int32(8), GroundLevel + 8, int32(8)
	var states []uint16
	for _, thickness := range []string{"base", "middle", "frustum", "tip"} {
		states = append(states, registry.MustState("minecraft:pointed_dripstone", map[string]string{
			"vertical_direction": "down", "thickness": thickness, "waterlogged": "true",
		}))
	}
	changes := make([]BlockChange, 0, len(states))
	falls := make([]FallingSpawn, 0, len(states))
	b.ReportAllocs()
	for b.Loop() {
		for i, state := range states {
			w.SetBlock(x, top-int32(i), z, state)
		}
		changes = changes[:0]
		falls = falls[:0]
		p.fallStalactite(x, top, z, states[0], &changes, &falls)
	}
	if len(falls) != len(states) {
		b.Fatalf("got %d falls, want %d", len(falls), len(states))
	}
}

// BenchmarkNeighbourChange is one neighbour change from below on a block
// standing on the flat world's ground: the neighbour update and the shape
// update a change wakes.
func BenchmarkNeighbourChange(b *testing.B) {
	w, _ := NewWorld(b.TempDir(), NewFlatGenerator())
	b.Cleanup(func() { _ = w.Close() })
	w.Acquire(ChunkPos{})
	p := NewPhysics(w)
	stone := registry.BlockStates["minecraft:stone"]
	w.SetBlock(1, GroundLevel-1, 1, registry.BlockStates["minecraft:dirt"])
	for _, name := range []string{"stone", "torch", "poppy", "rail", "oak_sign"} {
		state := registry.BlockStates["minecraft:"+name]
		w.SetBlock(1, GroundLevel, 1, state)
		b.Run(name, func(b *testing.B) {
			b.ReportAllocs()
			var changes []BlockChange
			for range b.N {
				changes = changes[:0]
				p.updateAsNeighbor(1, GroundLevel, 1, &changes, false, stone, stone, [3]int32{0, -1, 0})
				p.shapeUpdate(1, GroundLevel, 1, [3]int32{0, 1, 0}, &changes)
			}
			if w.BlockAt(1, GroundLevel, 1) != state || len(changes) != 0 {
				b.Fatalf("%s changed: %v", name, changes)
			}
		})
	}
}
