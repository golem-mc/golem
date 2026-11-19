package world

import (
	"testing"

	"github.com/golem-mc/golem/registry"
)

// BenchmarkCrafterPowerEdges alternates the neighbor signal and runs the crafter's reaction.
func BenchmarkCrafterPowerEdges(b *testing.B) {
	w, err := NewWorld(b.TempDir(), NewFlatGenerator())
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { _ = w.Close() })
	w.Acquire(ChunkPos{})
	p := NewPhysics(w)
	const x, y, z = 5, GroundLevel, 5
	w.SetBlock(x, y, z, registry.BlockCrafter.DefaultState())
	w.EnsureBlockEntity(x, y, z, KindCrafter)
	var changes []BlockChange
	on := false
	b.ReportAllocs()
	for b.Loop() {
		on = !on
		source := registry.BlockAir
		if on {
			source = registry.BlockRedstoneBlock
		}
		w.SetBlock(x+1, y, z, source.DefaultState())
		changes = changes[:0]
		p.redstoneNeighborChanged(x, y, z, w.BlockAt(x, y, z), &changes, false)
		// Consume the scheduled visits so the queue stays bounded.
		p.now += crafterDelay
		if len(p.queue) > 0 {
			delete(p.pending, p.queue.pop().pos)
		}
		w.containerChanges = w.containerChanges[:0]
	}
}
