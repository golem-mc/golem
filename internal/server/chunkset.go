package server

import (
	"iter"
	"math/bits"

	"github.com/golem-mc/golem/internal/server/world"
)

// chunkSet wraps absolute chunk coordinates into a power-of-two bit window.
// Reframe only after unloading departures: retained bits then keep their
// coordinates when the window moves, without copying the overlapping area.
type chunkSet struct {
	center       world.ChunkPos
	radius, side int32
	words        []uint64
	count        int
}

func newChunkSet(center world.ChunkPos, radius int32) *chunkSet {
	side := int32(1) << bits.Len32(uint32(2*radius))
	return &chunkSet{center: center, radius: radius, side: side, words: make([]uint64, (int(side)*int(side)+63)/64)}
}

func (s *chunkSet) reframe(center world.ChunkPos, radius int32) *chunkSet {
	if s == nil {
		return newChunkSet(center, radius)
	}
	if int32(1)<<bits.Len32(uint32(2*radius)) != s.side {
		next := newChunkSet(center, radius)
		for p := range s.all() {
			next.add(p)
		}
		return next
	}
	s.center, s.radius = center, radius
	return s
}

func (s *chunkSet) bit(p world.ChunkPos) (int, uint64) {
	mask := s.side - 1
	i := int((p.X&mask)*s.side + (p.Z & mask))
	return i >> 6, uint64(1) << (i & 63)
}

func (s *chunkSet) has(p world.ChunkPos) bool {
	if s == nil || !world.InViewOf(p, s.center, s.radius) {
		return false
	}
	i, mask := s.bit(p)
	return s.words[i]&mask != 0
}

func (s *chunkSet) add(p world.ChunkPos) {
	i, mask := s.bit(p)
	if s.words[i]&mask == 0 {
		s.count++
		s.words[i] |= mask
	}
}

func (s *chunkSet) delete(p world.ChunkPos) {
	if !s.has(p) {
		return
	}
	i, mask := s.bit(p)
	s.words[i] &^= mask
	s.count--
}

func (s *chunkSet) len() int {
	if s == nil {
		return 0
	}
	return s.count
}

func (s *chunkSet) all() iter.Seq[world.ChunkPos] {
	return func(yield func(world.ChunkPos) bool) {
		if s == nil {
			return
		}
		loX, loZ, mask := s.center.X-s.radius, s.center.Z-s.radius, s.side-1
		shift := bits.TrailingZeros32(uint32(s.side))
		for word, value := range s.words {
			for value != 0 {
				bit := bits.TrailingZeros64(value)
				value &^= uint64(1) << bit
				i := int32(word*64 + bit)
				p := world.ChunkPos{X: loX + ((i>>shift - loX) & mask), Z: loZ + ((i - loZ) & mask)}
				if !yield(p) {
					return
				}
			}
		}
	}
}
