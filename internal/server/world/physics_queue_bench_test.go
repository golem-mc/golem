package world

import (
	"strconv"
	"testing"

	"github.com/golem-mc/golem/registry"
)

func BenchmarkComparatorDistantFluidTicks(b *testing.B) {
	for _, n := range []int32{0, 4096, 65536} {
		b.Run(strconv.Itoa(int(n)), func(b *testing.B) {
			w := newTestWorld(b, NewFlatGenerator())
			b.Cleanup(func() { w.Close() })
			p := NewPhysics(w)
			p.mu.Lock()
			defer p.mu.Unlock()
			water := fluidTypeOf(fluidHolding(waterByLevel[0]))
			for i := range n {
				p.insertTick(blockKey{16 + i%256, 64, i / 256}, water, 200, tickPriorityNormal)
			}
			state := registry.BlockComparator.DefaultState()
			b.ReportAllocs()
			for b.Loop() {
				p.comparatorCheckTickOnNeighbor(0, 64, 0, state, 100)
			}
		})
	}
}

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

// BenchmarkDistantComparatorTouches touches 1,024 hoppers per tick, with no
// comparator and with one outside every hopper's reach.
func BenchmarkDistantComparatorTouches(b *testing.B) {
	for _, name := range []string{"empty", "distant"} {
		b.Run(name, func(b *testing.B) {
			w := newTestWorld(b, NewFlatGenerator())
			b.Cleanup(func() { w.Close() })
			for cx := range int32(2) {
				for cz := range int32(2) {
					w.Acquire(ChunkPos{X: cx, Z: cz})
				}
			}
			w.Acquire(ChunkPos{X: 12, Z: 0})
			hopper := registry.BlockHopper.DefaultState()
			for x := range int32(32) {
				for z := range int32(32) {
					w.SetBlock(x, 64, z, hopper)
				}
			}
			if name == "distant" {
				w.SetBlock(192, 64, 0, registry.BlockComparator.DefaultState())
			}
			w.FlushLighting()
			p := NewPhysics(w)
			var now int64
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				w.TickTime()
				now++
				for x := range int32(32) {
					for z := range int32(32) {
						w.Touch(x, 64, z)
					}
				}
				p.Tick(now)
			}
		})
	}
}
