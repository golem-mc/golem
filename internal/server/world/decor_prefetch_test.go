package world

import (
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
)

type prefetchSource struct {
	active atomic.Int32
	peak   atomic.Int32
	mu     sync.Mutex
	caller string
	inline bool
	calls  []ChunkPos
	shaped [25]shapedTerrain
	center *Chunk
	cached int
}

func (s *prefetchSource) index(x, z int32) int { return int((z+2)*5 + x + 2) }
func (s *prefetchSource) cachedTerrain(x, z int32) *shapedTerrain {
	if s.index(x, z) == s.cached {
		return &s.shaped[s.cached]
	}
	return nil
}
func (s *prefetchSource) terrainOf(x, z int32, c *Chunk) *shapedTerrain {
	v := s.active.Add(1)
	defer s.active.Add(-1)
	for old := s.peak.Load(); v > old; old = s.peak.Load() {
		if s.peak.CompareAndSwap(old, v) {
			break
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.inline = s.inline && goroutineMark() == s.caller
	s.calls = append(s.calls, ChunkPos{X: x, Z: z})
	if x == 0 && z == 0 && c != s.center {
		panic("center not reused")
	}
	if (x != 0 || z != 0) && c != nil {
		panic("halo received center")
	}
	return &s.shaped[s.index(x, z)]
}
func (*prefetchSource) decor() *decorState                                { return nil }
func (*prefetchSource) isAir(uint16) bool                                 { return false }
func (*prefetchSource) biomeIndex(*decorRegion, int32, int32, int32) uint { return 0 }

func TestDecorPrefetchStaysInline(t *testing.T) {
	for _, cores := range []int{1, 4} {
		t.Run(string(rune('0'+cores)), func(t *testing.T) {
			old := runtime.GOMAXPROCS(cores)
			defer runtime.GOMAXPROCS(old)
			c := &Chunk{}
			s := &prefetchSource{caller: goroutineMark(), inline: true, center: c, cached: 3}
			r := newDecorRegion(s, c, worldMinY, worldHeight)
			if !s.inline || s.peak.Load() != 1 {
				t.Fatal("halo shaping escaped its caller")
			}
			call := 0
			for i, got := range r.near {
				if got != &s.shaped[i] {
					t.Fatalf("slot %d has wrong terrain", i)
				}
				if i == s.cached {
					continue
				}
				want := ChunkPos{X: int32(i%5) - 2, Z: int32(i/5) - 2}
				if call >= len(s.calls) || s.calls[call] != want {
					t.Fatalf("call %d: %v, want %v", call, s.calls, want)
				}
				call++
			}
			if len(s.calls) != 24 {
				t.Fatalf("%d shaping calls, want 24", len(s.calls))
			}
		})
	}
}
