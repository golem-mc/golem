package world

import "github.com/golem-mc/golem/registry"

// biomeSnapshot is an immutable copy of a section's raw quart cells. A uniform
// section needs no cell array; a mixed section never aliases mutable storage.
type biomeSnapshot struct {
	uniform int32
	cells   *[biomesPerSection]int32
}

func (g *biomeSnapshot) at(x, y, z int32) int32 {
	if g.cells == nil {
		return g.uniform
	}
	return g.cells[(y&3)*16+z*4+x]
}

// cornersAgree proves the selected biome without computing the jittered corner.
// All eight parents must belong to this grid; low/high are already clamped.
// A non-nil reps compares and returns representatives, and then needs every
// parent to be a valid biome; nil compares raw IDs, sentinels included.
func (g *biomeSnapshot) cornersAgree(x, low, z, high int32, reps *[registry.BiomeCount]registry.Biome) (int32, bool) {
	b := g.read(x, low, z, reps)
	if b == invalidBiomeRead {
		return 0, false
	}
	for _, y := range [2]int32{low, high} {
		for dx := range int32(2) {
			for dz := range int32(2) {
				if g.read(x+dx, y, z+dz, reps) != b {
					return 0, false
				}
			}
		}
	}
	return b, true
}

// invalidBiomeRead marks an unmappable parent, which proves nothing.
const invalidBiomeRead = -2

// read is the raw cell, or its representative; an invalid ID never maps.
func (g *biomeSnapshot) read(x, y, z int32, reps *[registry.BiomeCount]registry.Biome) int32 {
	b := g.at(x, y, z)
	if reps == nil {
		return b
	}
	if b < 0 || b >= registry.BiomeCount {
		return invalidBiomeRead
	}
	return int32(reps[b])
}

func (w *World) rawBiomeGrid(s *section) *biomeSnapshot {
	if g := s.biomeGrid.Load(); g != nil {
		return g
	}
	w.mu.RLock()
	defer w.mu.RUnlock()
	if g := s.biomeGrid.Load(); g != nil {
		return g
	}
	g := &biomeSnapshot{uniform: s.biome}
	if s.biomes != nil {
		g.cells = new([biomesPerSection]int32)
		copy(g.cells[:], s.biomes)
	}
	if s.biomeGrid.CompareAndSwap(nil, g) {
		return g
	}
	return s.biomeGrid.Load()
}

// biomeChunk fills near slots on collisions without retaining unloaded misses.
func (w *World) biomeChunk(pos ChunkPos) *loadedChunk {
	if l := w.nearAt(pos); l != nil {
		return l
	}
	w.mu.RLock()
	l := w.chunks[pos]
	if l != nil {
		w.near[nearIndex(pos)].Store(l)
	}
	w.mu.RUnlock()
	return l
}
