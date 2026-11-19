package world

import (
	"sync"
	"sync/atomic"
	"testing"

	"github.com/golem-mc/golem/registry"
)

// BenchmarkBlockAtDuringRelight is the cost of a block read while relights
// run back to back beside it, the tick's reads against the light lane: a
// relight that held the world's write lock for its whole recompute made each
// read wait out the rest of it.
func BenchmarkBlockAtDuringRelight(b *testing.B) {
	w, _ := NewWorld(b.TempDir(), NewFlatGenerator())
	b.Cleanup(func() { _ = w.Close() })
	for x := int32(-3); x <= 3; x++ {
		for z := int32(-3); z <= 3; z++ {
			w.Acquire(ChunkPos{X: x, Z: z})
		}
	}
	glow := registry.BlockStates["minecraft:glowstone"]
	var stop atomic.Bool
	var wg sync.WaitGroup
	wg.Go(func() {
		state := glow
		for !stop.Load() {
			w.SetBlock(8, GroundLevel+4, 8, state)
			w.Relight(8, 8)
			state ^= glow // glowstone and air in turn
		}
	})
	b.Cleanup(func() {
		stop.Store(true)
		wg.Wait()
	})

	b.ResetTimer()
	for i := range b.N {
		w.BlockAt(int32(i&31)-16, GroundLevel-1, 0)
	}
}

// BenchmarkRelight is the cost of one relight after a light-changing write.
func BenchmarkRelight(b *testing.B) {
	w, _ := NewWorld(b.TempDir(), NewFlatGenerator())
	b.Cleanup(func() { _ = w.Close() })
	for x := int32(-3); x <= 3; x++ {
		for z := int32(-3); z <= 3; z++ {
			w.Acquire(ChunkPos{X: x, Z: z})
		}
	}
	glow := registry.BlockStates["minecraft:glowstone"]
	state := glow
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		w.SetBlock(8, GroundLevel+4, 8, state)
		w.Relight(8, 8)
		state ^= glow
	}
}
