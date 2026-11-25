package world

import "testing"

// BenchmarkChunkTicks times the snapshot of a chunk with queued block and
// fluid ticks, which runs under the physics mutex.
func BenchmarkChunkTicks(b *testing.B) {
	w, err := NewWorld(b.TempDir(), NewFlatGenerator())
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { w.Close() })
	w.Acquire(ChunkPos{})
	p := NewPhysics(w)
	water := fluidType(fluidWater)<<1 | 1
	p.mu.Lock()
	for i := range 256 {
		x, z := int32(i%16), int32(i/16)
		p.insertTick(blockKey{x, GroundLevel, z}, water, int64(100-i%50), 0)
		p.insertTick(blockKey{x, GroundLevel + 1, z}, 0, int64(100-i%40), 0)
	}
	p.mu.Unlock()
	b.ReportAllocs()
	for b.Loop() {
		p.chunkTicks(ChunkPos{})
	}
}
