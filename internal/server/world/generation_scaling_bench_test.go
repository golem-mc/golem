package world

import (
	"runtime"
	"sync"
	"testing"
)

// BenchmarkOverworldGeneration1000 includes terrain and decoration of a fresh
// 40x25 rectangle. Every iteration generates exactly 1,000 requested chunks;
// neighbouring terrain and decorations are included in the cost.
func BenchmarkOverworldGeneration1000(b *testing.B) {
	cores := runtime.GOMAXPROCS(0)
	for _, tc := range []struct {
		name          string
		cpus, runners int
	}{
		{"OneCPU", 1, 1},
		{"Runners8", cores, 8},
		{"Runners16", cores, 16},
		{"AllCPUs", cores, cores},
	} {
		b.Run(tc.name, func(b *testing.B) {
			previous := runtime.GOMAXPROCS(tc.cpus)
			defer runtime.GOMAXPROCS(previous)
			NewOverworldGenerator(5).Generate(newChunk(1000, 1000)) // shared structure data
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				g := NewOverworldGenerator(5)
				jobs := make(chan ChunkPos, 1000)
				for z := range int32(25) {
					for x := range int32(40) {
						jobs <- ChunkPos{X: x, Z: z}
					}
				}
				close(jobs)
				var wg sync.WaitGroup
				for range tc.runners {
					wg.Go(func() {
						for p := range jobs {
							g.Generate(newChunk(p.X, p.Z))
						}
					})
				}
				wg.Wait()
			}
			b.ReportMetric(float64(b.N*1000)/b.Elapsed().Seconds(), "chunks/s")
		})
	}
}
