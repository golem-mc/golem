package world

import (
	"testing"

	"github.com/golem-mc/golem/registry"
)

// BenchmarkBoneMealGrowth visits crop, double-plant and vine growth, resetting each target.
func BenchmarkBoneMealGrowth(b *testing.B) {
	for _, block := range []registry.Block{registry.BlockWheat, registry.BlockShortGrass, registry.BlockTwistingVines, registry.BlockPitcherCrop} {
		b.Run(block.Name(), func(b *testing.B) {
			w, err := NewWorld(b.TempDir(), NewFlatGenerator())
			if err != nil {
				b.Fatal(err)
			}
			b.Cleanup(func() { _ = w.Close() })
			w.Acquire(ChunkPos{})
			p := NewPhysics(w)
			state := block.DefaultState()
			w.SetBlock(8, GroundLevel-1, 8, registry.BlockFarmland.DefaultState())
			b.ReportAllocs()
			var changes []BlockChange
			for b.Loop() {
				w.SetBlock(8, GroundLevel, 8, state)
				w.SetBlock(8, GroundLevel+1, 8, registry.BlockAir.DefaultState())
				changes = changes[:0]
				p.growCrop(8, GroundLevel, 8, state, &changes)
			}
		})
	}
}
