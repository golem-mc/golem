package world

import (
	"sync"
	"sync/atomic"
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
