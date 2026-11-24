package world

import (
	"testing"

	"github.com/golem-mc/golem/registry"
)

// BenchmarkScheduledTickQueue books 4096 stone visits and pops them next tick;
// stone visits do nothing, leaving the scheduling cost.
func BenchmarkScheduledTickQueue(b *testing.B) {
	w := newTestWorld(b, NewFlatGenerator())
	b.Cleanup(func() { w.Close() })
	for cx := range int32(4) {
		for cz := range int32(4) {
			w.Acquire(ChunkPos{X: cx, Z: cz})
		}
	}
	w.FlushLighting()
	p := NewPhysics(w)
	var now int64
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		now++
		p.mu.Lock()
		for x := range int32(64) {
			for z := range int32(64) {
				p.schedule(x, GroundLevel-2, z, now)
			}
		}
		p.mu.Unlock()
		p.Tick(now)
	}
	b.StopTimer()
	b.ReportMetric(float64(b.Elapsed().Nanoseconds())/float64(b.N)/4096, "ns/visit")
}

// BenchmarkScheduledTickQueueHeld ticks with 4096 due visits in chunks the
// tick filter refuses; they must cost nothing per tick while held.
func BenchmarkScheduledTickQueueHeld(b *testing.B) {
	w := newTestWorld(b, NewFlatGenerator())
	b.Cleanup(func() { w.Close() })
	for cx := range int32(4) {
		for cz := range int32(4) {
			w.Acquire(ChunkPos{X: cx, Z: cz})
		}
	}
	w.FlushLighting()
	p := NewPhysics(w)
	p.SetTickFilter(func(ChunkPos) bool { return false })
	p.mu.Lock()
	for x := range int32(64) {
		for z := range int32(64) {
			p.schedule(x, GroundLevel-2, z, 1)
		}
	}
	p.mu.Unlock()
	var now int64 = 1
	p.Tick(now)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		now++
		p.Tick(now)
	}
}

// BenchmarkScheduledDirtPathTick books 4096 dirt path ticks and pops them next
// tick, so the path's own dispatch branch runs.
func BenchmarkScheduledDirtPathTick(b *testing.B) {
	w := newTestWorld(b, NewFlatGenerator())
	b.Cleanup(func() { w.Close() })
	for cx := range int32(4) {
		for cz := range int32(4) {
			w.Acquire(ChunkPos{X: cx, Z: cz})
		}
	}
	w.FlushLighting()
	p := NewPhysics(w)
	var now int64
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		now++
		b.StopTimer()
		for x := range int32(64) {
			for z := range int32(64) {
				w.SetBlock(x, GroundLevel-2, z, registry.BlockDirtPath.DefaultState())
			}
		}
		b.StartTimer()
		p.mu.Lock()
		for x := range int32(64) {
			for z := range int32(64) {
				p.scheduleSoil(registry.BlockDirtPath, x, GroundLevel-2, z, now, tickPriorityNormal)
			}
		}
		p.mu.Unlock()
		p.Tick(now)
	}
	b.StopTimer()
	b.ReportMetric(float64(b.Elapsed().Nanoseconds())/float64(b.N)/4096, "ns/visit")
}
