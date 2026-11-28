package world

import (
	"math"
	"runtime"
	"sync"
	"testing"
)

// BenchmarkOverworldGenerateSpread interleaves six distant regions across workers.
func BenchmarkOverworldGenerateSpread(b *testing.B) { benchmarkGenerateSpread(b, 6, false) }

// BenchmarkOverworldGenerateSpreadSpilled interleaves twelve regions with a
// proto directory attached and a cap of 384, so the live protos pass it and
// spill.
func BenchmarkOverworldGenerateSpreadSpilled(b *testing.B) { benchmarkGenerateSpread(b, 12, true) }

// BenchmarkOverworldGenerateViews interleaves twenty 21x21 views, as players
// in fresh terrain far apart load them; shapes/chunk is 25x25/21x21 at best.
func BenchmarkOverworldGenerateViews(b *testing.B) {
	const regions = 20
	lists := make([][]ChunkPos, regions)
	for k := range lists {
		angle := float64(k) * 2 * math.Pi / regions
		cx, cz := int32(math.Round(187.5*math.Cos(angle))), int32(math.Round(187.5*math.Sin(angle)))
		for ring := int32(0); ring <= 10; ring++ {
			for x := -ring; x <= ring; x++ {
				for z := -ring; z <= ring; z++ {
					if max(absInt32(x), absInt32(z)) == ring {
						lists[k] = append(lists[k], ChunkPos{X: cx + x, Z: cz + z})
					}
				}
			}
		}
	}
	warm := NewOverworldGenerator(8675309)
	warm.Generate(newChunk(1000, 1000)) // warm shared structure data
	var shapes, cold int64
	chunks := regions * 441
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		g := NewOverworldGenerator(8675309)
		jobs := make(chan ChunkPos, chunks)
		for i := range lists[0] {
			for k := range lists {
				jobs <- lists[k][i]
			}
		}
		close(jobs)
		var wg sync.WaitGroup
		for range max(1, runtime.GOMAXPROCS(0)-2) {
			wg.Go(func() {
				for p := range jobs {
					g.Generate(newChunk(p.X, p.Z))
				}
			})
		}
		wg.Wait()
		shapes += g.shapes.Load()
		g.cache.mu.Lock()
		cold += int64(g.cache.coldBytes)
		g.cache.mu.Unlock()
	}
	b.ReportMetric(float64(shapes)/float64(b.N*chunks), "shapes/chunk")
	b.ReportMetric(float64(cold)/float64(b.N), "coldbytes")
}

func benchmarkGenerateSpread(b *testing.B, regions int, spill bool) {
	g := NewOverworldGenerator(8675309)
	g.Generate(newChunk(1000, 1000)) // warm shared structure data
	lists := make([][]ChunkPos, regions)
	for k := range lists {
		angle := float64(k) * 2 * math.Pi / float64(regions)
		cx, cz := int32(math.Round(375*math.Cos(angle))), int32(math.Round(375*math.Sin(angle)))
		for ring := int32(0); ring <= 3; ring++ {
			for x := -ring; x <= ring; x++ {
				for z := -ring; z <= ring; z++ {
					if max(absInt32(x), absInt32(z)) == ring {
						lists[k] = append(lists[k], ChunkPos{X: cx + x, Z: cz + z})
					}
				}
			}
		}
	}
	var shapes, spills, readbacks int64
	chunks := regions * 49
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		g = NewOverworldGenerator(8675309)
		if spill {
			g.OpenProtos(b.TempDir())
			g.protos.cap = 384 // twelve regions stay under decorProtoCap
		}
		jobs := make(chan ChunkPos, chunks)
		for i := range lists[0] {
			for k := range lists {
				jobs <- lists[k][i]
			}
		}
		close(jobs)
		var wg sync.WaitGroup
		for range max(1, runtime.GOMAXPROCS(0)-2) {
			wg.Go(func() {
				for p := range jobs {
					g.Generate(newChunk(p.X, p.Z))
				}
			})
		}
		wg.Wait()
		shapes += g.shapes.Load()
		spills += g.protos.spills.Load()
		readbacks += g.protos.readbacks.Load()
	}
	n := float64(b.N * chunks)
	b.ReportMetric(float64(shapes)/n, "shapes/chunk")
	if spill {
		b.ReportMetric(float64(spills)/n, "spills/chunk")
		b.ReportMetric(float64(readbacks)/n, "readbacks/chunk")
	}
}
