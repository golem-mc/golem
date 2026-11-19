package world

import (
	"testing"

	"github.com/golem-mc/golem/registry"
)

// BenchmarkShulkerLidCycle opens and closes one box through its physics ticks,
// including the neighbour updates that read the lid's conductor predicate.
func BenchmarkShulkerLidCycle(b *testing.B) {
	w := newTestWorld(b, NewFlatGenerator())
	w.Acquire(ChunkPos{})
	defer w.Release(ChunkPos{})
	w.SetBlock(8, 80, 8, registry.BlockShulkerBox.DefaultState())
	p := NewPhysics(w)
	var clock int64
	b.ReportAllocs()
	for b.Loop() {
		for _, viewers := range []int{1, 0} {
			p.ShulkerViewersChanged(8, 80, 8, viewers)
			for range shulkerLidTicks {
				clock++
				p.Tick(clock)
				p.TakeShulkerShoves()
			}
		}
	}
}
