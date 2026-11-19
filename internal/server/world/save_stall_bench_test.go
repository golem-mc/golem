package world

import (
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/golem-mc/golem/registry"
)

const saveStallChunks = 344

// dirtyChunks loads an overworld square of about n chunks and marks each
// dirty with a chest, which is what an autosave interval's worth of building
// leaves behind.
func dirtyChunks(tb testing.TB, w *World, n int) []ChunkPos {
	tb.Helper()
	var all []ChunkPos
	for x := int32(-10); len(all) < n; x++ {
		for z := int32(-10); z < 10 && len(all) < n; z++ {
			all = append(all, ChunkPos{X: x, Z: z})
		}
	}
	chest := registry.BlockStates["minecraft:chest"]
	for _, pos := range all {
		w.Acquire(pos)
	}
	for _, pos := range all {
		x, z := pos.X*16+3, pos.Z*16+3
		y := w.SurfaceHeight(x, z) + 1
		w.SetBlock(x, y, z, chest)
		w.EnsureBlockEntity(x, y, z, KindChest)
	}
	return all
}

// BenchmarkSaveStall saves 344 dirty overworld chunks while a probe takes the
// world's write lock in a loop, the way a tick's SetBlock does. The probe's
// worst wait is the longest the tick could be held up by the save.
func BenchmarkSaveStall(b *testing.B) {
	for _, sync_ := range []bool{true, false} {
		name := "nosync"
		if sync_ {
			name = "sync"
		}
		b.Run(name, func(b *testing.B) {
			var lastWall time.Duration
			var waits []time.Duration
			for b.Loop() {
				b.StopTimer()
				w, _ := NewWorld(b.TempDir(), NewOverworldGenerator(5))
				if err := w.SetSaveFormat("deflate", sync_); err != nil {
					b.Fatal(err)
				}
				all := dirtyChunks(b, w, saveStallChunks)
				b.StartTimer()

				stop := make(chan struct{})
				var wg sync.WaitGroup
				var mu sync.Mutex
				wg.Go(func() {
					for {
						select {
						case <-stop:
							return
						default:
						}
						start := time.Now()
						w.mu.Lock()
						d := time.Since(start)
						w.mu.Unlock()
						mu.Lock()
						waits = append(waits, d)
						mu.Unlock()
						time.Sleep(200 * time.Microsecond)
					}
				})
				t0 := time.Now()
				w.Save()
				lastWall = time.Since(t0)
				close(stop)
				wg.Wait()
				b.StopTimer()
				_ = all
				w.Close()
				b.StartTimer()
			}
			slices.Sort(waits)
			b.ReportMetric(float64(lastWall.Milliseconds()), "save-wall-ms")
			b.ReportMetric(float64(waits[len(waits)-1].Microseconds())/1000, "max-lock-wait-ms")
			b.ReportMetric(float64(waits[len(waits)*99/100].Microseconds())/1000, "p99-lock-wait-ms")
		})
	}
}
