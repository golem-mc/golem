package world

import (
	"testing"

	"github.com/golem-mc/golem/registry"
)

// BenchmarkWakeNeighborsNoHooks is the six-neighbour fan-out of one block
// change with no BlockHooks set, the cost every plugin-free server pays for the
// physics event's gate.
func BenchmarkWakeNeighborsNoHooks(b *testing.B) {
	w, err := NewWorld(b.TempDir(), NewFlatGenerator())
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { _ = w.Close() })
	w.Acquire(ChunkPos{})
	p := NewPhysics(w)
	stone := registry.BlockStone.DefaultState()
	w.SetBlock(8, GroundLevel+1, 8, stone)
	var changes []BlockChange
	b.ReportAllocs()
	for b.Loop() {
		p.wakeNeighbors(8, GroundLevel+1, 8, 0, stone, &changes)
		changes = changes[:0]
	}
}
