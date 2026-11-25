package world

import (
	"fmt"
	"github.com/golem-mc/golem/registry"
	"testing"
)

// BenchmarkSectionWrites isolates repeated changed-block writes at each palette
// width, including section counters but without world locking, lighting or
// packet cache invalidation.
func BenchmarkSectionWrites(b *testing.B) {
	for _, states := range []int{16, 32, 64, 128, 256, 257} {
		for _, shared := range []bool{false, true} {
			b.Run(fmt.Sprintf("%d/shared=%t", states, shared), func(b *testing.B) {
				var s section
				for i := range blocksPerSection {
					s.set(i, uint16(i%states), shared)
				}
				b.ReportAllocs()
				b.ResetTimer()
				for i := range b.N {
					cell := i * 977 & (blocksPerSection - 1)
					// Advance the state each sweep so writes keep changing cells.
					s.set(cell, uint16((i+i/blocksPerSection+1)%states), shared)
				}
			})
		}
	}
}

// BenchmarkBlockAtPackedSections reads changing cells of a loaded section at
// every storage width, including the uniform and global-ID forms.
func BenchmarkBlockAtPackedSections(b *testing.B) {
	for _, states := range []int{1, 16, 17, 33, 65, 129, 257} {
		b.Run(fmt.Sprint(states), func(b *testing.B) {
			w, err := NewWorld(b.TempDir(), NewFlatGenerator())
			if err != nil {
				b.Fatal(err)
			}
			b.Cleanup(func() { _ = w.Close() })
			w.Acquire(ChunkPos{})
			for i := range blocksPerSection {
				w.SetBlock(int32(i&15), 64+int32(i>>8), int32(i>>4&15), uint16(i%states))
			}
			b.ReportAllocs()
			b.ResetTimer()
			var sink uint16
			for i := range b.N {
				cell := i * 977 & (blocksPerSection - 1)
				sink += w.BlockAt(int32(cell&15), 64+int32(cell>>8), int32(cell>>4&15))
			}
			_ = sink
		})
	}
}

// BenchmarkRecountSection times recalcCounts of a four-bit section holding six
// terrain states in successive groups of 16 cells.
func BenchmarkRecountSection(b *testing.B) {
	states := [...]uint16{
		registry.BlockAir.DefaultState(), registry.BlockStone.DefaultState(),
		registry.BlockDirt.DefaultState(), registry.BlockGrassBlock.DefaultState(),
		registry.BlockWater.DefaultState(), registry.BlockOakLeaves.DefaultState(),
	}
	words := newBlockWords(states[0])
	for i := range blocksPerSection {
		words = words.put(i, states[i/16%len(states)], false)
	}
	var s section
	s.blocks.Store(words)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		s.recalcCounts()
	}
}
