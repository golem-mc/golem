package world

import (
	"testing"

	"github.com/golem-mc/golem/registry"
)

// BenchmarkCommandPlantSurvival includes a command's placement wake and the
// following physics tick, both with and without a surviving cactus.
func BenchmarkCommandPlantSurvival(b *testing.B) {
	for _, base := range []registry.Block{registry.BlockStone, registry.BlockSand} {
		b.Run(base.Name(), func(b *testing.B) {
			w := newTestWorld(b, NewFlatGenerator())
			b.Cleanup(func() { _ = w.Close() })
			w.Acquire(ChunkPos{})
			w.SetBlock(8, GroundLevel-1, 8, base.DefaultState())
			w.FlushLighting()
			p := NewPhysics(w)
			var now int64
			b.ReportAllocs()
			for b.Loop() {
				w.SetBlock(8, GroundLevel, 8, registry.BlockCactus.DefaultState())
				p.NotifyPlaced(8, GroundLevel, 8, airState)
				now++
				p.Tick(now)
				w.SetBlock(8, GroundLevel, 8, airState)
			}
		})
	}
}
