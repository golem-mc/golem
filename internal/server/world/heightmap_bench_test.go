package world

import (
	"testing"

	"github.com/golem-mc/golem/registry"
)

// BenchmarkEncodeHeightmaps is the three client heightmaps of one flat chunk,
// built each time a chunk is generated, loaded or changed.
func BenchmarkEncodeHeightmaps(b *testing.B) {
	w, _ := NewWorld(b.TempDir(), NewFlatGenerator())
	b.Cleanup(func() { _ = w.Close() })
	loaded, _ := w.Acquire(ChunkPos{})
	b.ReportAllocs()
	for range b.N {
		w.encodeHeightmaps(loaded)
	}
}

// BenchmarkVoidChunkHeightmaps is the three client heightmaps of an empty End
// chunk, whose sections are all uniform air.
func BenchmarkVoidChunkHeightmaps(b *testing.B) {
	w := &World{dim: dimensionShapes[EndName]}
	c := newChunk(0, 0)
	b.ReportAllocs()
	for range b.N {
		w.encodeHeightmaps(c)
	}
}

// BenchmarkMotionHeightWrites is one block write per iteration in a column
// whose cached MOTION_BLOCKING height a write either leaves alone (below a
// roof) or moves (toggling the top).
func BenchmarkMotionHeightWrites(b *testing.B) {
	stone := registry.BlockStates["minecraft:stone"]
	newColumn := func(b *testing.B) *World {
		w, _ := NewWorld(b.TempDir(), NewFlatGenerator())
		b.Cleanup(func() { _ = w.Close() })
		w.Acquire(ChunkPos{})
		w.SwapBlock(8, 200, 8, stone)
		return w
	}
	b.Run("below-roof", func(b *testing.B) {
		w := newColumn(b)
		dirt := registry.BlockStates["minecraft:dirt"]
		b.ReportAllocs()
		for i := range b.N {
			s := stone
			if i&1 == 0 {
				s = dirt
			}
			w.SwapBlock(8, 64, 8, s)
		}
	})
	b.Run("toggle-top", func(b *testing.B) {
		w := newColumn(b)
		water := waterByLevel[0]
		b.ReportAllocs()
		for i := range b.N {
			s, want := water, int32(201)
			if i&1 == 1 {
				s, want = airState, 200
			}
			w.SwapBlock(8, 201, 8, s)
			if i < 4 {
				if h := w.motionBlockingHeight(8, 8); h != want {
					b.Fatalf("height = %d, want %d", h, want)
				}
			}
		}
	})
}
