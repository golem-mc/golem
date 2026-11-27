package server

import (
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/server/world"
)

func revisitServer(tb testing.TB) *Server {
	tb.Helper()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.LevelName = tb.TempDir()
	cfg.LevelType = TestFlatLevelType
	s, err := newServer(cfg, 1)
	if err != nil {
		tb.Fatal(err)
	}
	tb.Cleanup(s.Shutdown)
	return s
}

// revisitSquare loads, ticks, unloads and ticks a 10x10 square of chunks.
func revisitSquare(s *Server, cycles int) {
	w := s.allDims()[0].w
	for range cycles {
		var held []world.ChunkPos
		for x := int32(100); x < 110; x++ {
			for z := int32(100); z < 110; z++ {
				pos := world.ChunkPos{X: x, Z: z}
				w.Acquire(pos)
				held = append(held, pos)
			}
		}
		s.tickMobs()
		for _, pos := range held {
			w.Release(pos)
		}
		w.FlushSaves()
		s.tickMobs()
	}
}

func TestRevisitedEmptyChunkLeavesWokenRecords(t *testing.T) {
	s := revisitServer(t)
	revisitSquare(s, 3)
	dim := s.allDims()[0]
	s.mobsMu.Lock()
	defer s.mobsMu.Unlock()
	if n := len(dim.mobs.woken); n != 0 {
		t.Errorf("len(woken) = %d, want 0", n)
	}
	for pos := range dim.mobs.listed {
		if pos.X >= 100 && pos.X < 110 && pos.Z >= 100 && pos.Z < 110 {
			t.Fatalf("chunk %v still listed", pos)
		}
	}
}

func BenchmarkLeaveUnloadedAfterRevisits(b *testing.B) {
	for _, stale := range []bool{false, true} {
		name := "Fresh"
		if stale {
			name = "Revisited"
		}
		b.Run(name, func(b *testing.B) {
			s := revisitServer(b)
			s.SetSpawning(false)
			if stale {
				// The records 10 000 revisited chunks left: woken, unlisted, unloaded.
				h := s.allDims()[0].mobs
				s.mobsMu.Lock()
				for x := range int32(100) {
					for z := range int32(100) {
						h.woken[world.ChunkPos{X: 1000 + x, Z: z}] = struct{}{}
					}
				}
				s.mobsMu.Unlock()
			}
			w := s.allDims()[0].w
			var held []world.ChunkPos
			for x := range int32(4) {
				for z := range int32(4) {
					pos := world.ChunkPos{X: x, Z: z}
					w.Acquire(pos)
					held = append(held, pos)
				}
			}
			b.Cleanup(func() {
				for _, pos := range held {
					w.Release(pos)
				}
			})
			s.mobTickForce = true
			s.tickMobs()
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				s.mobTickForce = true
				s.tickMobs()
			}
		})
	}
}
