package world

import (
	"math/rand"
	"testing"

	"github.com/golem-mc/golem/registry"
)

// BenchmarkSaplingGrowth includes feature placement, leaf settlement and the
// neighbour wakes, resetting only the blocks each growth changed.
func BenchmarkSaplingGrowth(b *testing.B) {
	w := newTestWorld(b, NewFlatGenerator())
	b.Cleanup(func() { _ = w.Close() })
	for dx := int32(-1); dx <= 1; dx++ {
		for dz := int32(-1); dz <= 1; dz++ {
			w.Acquire(ChunkPos{X: dx, Z: dz})
		}
	}
	p := NewPhysics(w)
	sapling, _ := registry.WithInt(registry.BlockOakSapling.DefaultState(), registry.PropStage, 1)
	var changes []BlockChange
	b.ReportAllocs()
	for b.Loop() {
		for _, c := range changes {
			w.SetBlock(c.X, c.Y, c.Z, airState)
		}
		changes = changes[:0]
		w.SetBlock(8, 99, 8, registry.BlockDirt.DefaultState())
		w.SetBlock(8, 100, 8, sapling)
		p.rng = rand.New(rand.NewSource(21))
		p.advanceTree(8, 100, 8, sapling, &changes)
		if registry.BlockOf(w.BlockAt(8, 100, 8)) != registry.BlockOakLog {
			b.Fatal("sapling did not grow")
		}
	}
}
