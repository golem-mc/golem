package world

import (
	"sync/atomic"

	"github.com/golem-mc/golem/internal/server/world/levelgen"
	"github.com/golem-mc/golem/registry"
)

// SpawnChunk holds an origin; other chunks use ordinary live world lookups.
type SpawnChunk struct {
	*World
	origin    *loadedChunk
	biomePos  [3]int32
	biome     registry.Biome
	biomeRead bool
	fortress  bool
}

// HoldSpawnChunk holds pos if loaded; a successful hold needs Close.
func (w *World) HoldSpawnChunk(pos ChunkPos) (SpawnChunk, bool) {
	w.mu.RLock()
	l, _ := w.loadedAt(pos)
	if l != nil {
		l.users.Add(1)
	}
	w.mu.RUnlock()
	if l == nil {
		return SpawnChunk{}, false
	}
	if l.surface.Load() == nil {
		w.SurfaceHeight(pos.X*16, pos.Z*16)
	}
	_, fortress := w.gen.(interface {
		FortressAt(x, y, z int32) (bool, bool)
	})
	return SpawnChunk{World: w, origin: l, fortress: fortress}, true
}

// Close releases the origin's user hold.
func (s *SpawnChunk) Close() { s.Release(s.origin.pos) }

// InOrigin reports whether the position belongs to the held chunk.
func (s *SpawnChunk) InOrigin(x, z int32) bool {
	return x>>4 == s.origin.pos.X && z>>4 == s.origin.pos.Z
}

// HasFortresses reports the underlying generator's fortress capability.
func (s *SpawnChunk) HasFortresses() bool { return s.fortress }

// BlockAt reads the origin's atomic section storage directly.
func (s *SpawnChunk) BlockAt(x, y, z int32) uint16 {
	if s.InOrigin(x, z) {
		return s.origin.chunk.sharedBlockAt(x&15, y, z&15)
	}
	return s.World.BlockAt(x, y, z)
}

// SurfaceHeight reads the live origin heightmap initialized at acquisition.
func (s *SpawnChunk) SurfaceHeight(x, z int32) int32 {
	if s.InOrigin(x, z) {
		return s.OriginSurfaceHeight(x&15, z&15)
	}
	return s.World.SurfaceHeight(x, z)
}

// OriginSurfaceHeight reads the initialized live height at origin-local coordinates.
func (s *SpawnChunk) OriginSurfaceHeight(x, z int32) int32 {
	return atomic.LoadInt32(&s.origin.surface.Load()[x][z])
}

// ChunkLoadedAt knows the held origin cannot unload.
func (s *SpawnChunk) ChunkLoadedAt(x, z int32) bool {
	return s.InOrigin(x, z) || s.nearAt(ChunkPos{X: x >> 4, Z: z >> 4}) != nil || s.World.ChunkLoadedAt(x, z)
}

// ResetSpawnStep discards the biome memo before a walk step or after a sink.
func (s *SpawnChunk) ResetSpawnStep() { s.biomeRead = false }

// Biome shares only the last position read during one synchronous walk step.
func (s *SpawnChunk) Biome(x, y, z int32) registry.Biome {
	pos := [3]int32{x, y, z}
	if !s.biomeRead || pos != s.biomePos {
		s.biome = s.World.biome(x, y, z, s.origin)
		s.biomePos, s.biomeRead = pos, true
	}
	return s.biome
}

// SpawnBiome is Biome mapped through representatives, with invalid IDs passed
// through. It skips the jittered corner only when every parent of one loaded
// grid maps to the same representative.
// vanilla: BiomeManager.getBiome
func (s *SpawnChunk) SpawnBiome(x, y, z int32, reps *[registry.BiomeCount]registry.Biome) registry.Biome {
	w := s.World
	minQY, maxQY := w.MinY()>>2, (w.MinY()+w.Height()-1)>>2
	px, py, pz := levelgen.ZoomParent(x, y, z)
	low, high := min(max(py, minQY), maxQY), min(max(py+1, minQY), maxQY)
	if px>>2 == (px+1)>>2 && pz>>2 == (pz+1)>>2 && low>>2 == high>>2 {
		if loaded := w.spawnBiomeChunk(ChunkPos{X: px >> 2, Z: pz >> 2}, s.origin); loaded != nil {
			grid := w.rawBiomeGrid(&loaded.chunk.sections[(low*4-worldMinY)/16])
			if grid.cells == nil {
				return mapSpawnBiome(registry.Biome(grid.uniform), reps)
			}
			if b, ok := grid.cornersAgree(px&3, low, pz&3, high, reps); ok {
				return registry.Biome(b)
			}
		}
	}
	return mapSpawnBiome(s.Biome(x, y, z), reps)
}

func mapSpawnBiome(b registry.Biome, reps *[registry.BiomeCount]registry.Biome) registry.Biome {
	if b < 0 || b >= registry.BiomeCount {
		return b
	}
	return reps[b]
}

// InitialSpawnBlocked is the initial spawn check's isRedstoneConductor.
// vanilla: NaturalSpawner.spawnCategoryForPosition
func (s *SpawnChunk) InitialSpawnBlocked(x, y, z int32) bool {
	if !s.InOrigin(x, z) {
		return s.ConductorAt(x, y, z)
	}
	if y < worldMinY || y >= worldMinY+worldHeight {
		return false
	}
	sec := &s.origin.chunk.sections[(y-worldMinY)/16]
	cache := sec.spawn.Load()
	if cache == &spawnAll || cache == &spawnNone {
		return cache == &spawnAll
	}
	i := int((y&15)*256 + (z&15)*16 + x&15)
	if cache != nil {
		if bits := cache[i>>5].Load() >> (2 * (i & 31)) & 3; bits != 0 {
			return s.spawnBlockedAt(x, y, z, bits)
		}
	}
	return s.spawnBlockedAt(x, y, z, s.initialSpawnBlocked(sec, i))
}

func (s *SpawnChunk) spawnBlockedAt(x, y, z int32, bits uint64) bool {
	if bits == 3 {
		return s.ConductorAt(x, y, z)
	}
	return bits == 2
}

// spawnCollision holds 32 two-bit cells per word, each independently 00
// unknown, 01 false, 10 true or 11 live; a nonzero word says nothing about its
// other cells. Shulker lid changes do not write the block state. Singleton
// arrays are never accessed.
type spawnCollision [128]atomic.Uint64

var spawnAll, spawnNone spawnCollision

// initialSpawnBlocked classifies storage and fills cell i under the world read lock.
func (s *SpawnChunk) initialSpawnBlocked(sec *section, i int) uint64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	cache := sec.spawn.Load()
	if cache == nil {
		kind := uint64(0)
		b := sec.blocks.Load()
		if b == nil {
			kind = spawnCollisionBits(sec.fill)
		} else if b.palette != nil {
			for _, state := range b.palette[:b.used] {
				kind |= spawnCollisionBits(uint16(state))
				if kind == 3 {
					break
				}
			}
		}
		switch kind {
		case 1:
			cache = &spawnNone
		case 2:
			cache = &spawnAll
		default:
			cache = new(spawnCollision)
		}
		if !sec.spawn.CompareAndSwap(nil, cache) {
			cache = sec.spawn.Load()
		}
	}
	if cache == &spawnAll {
		return 2
	}
	if cache == &spawnNone {
		return 1
	}
	shift := 2 * (i & 31)
	word := &cache[i>>5]
	if bits := word.Load() >> shift & 3; bits != 0 {
		return bits
	}
	b := sec.blocks.Load()
	// Two equal, repeated-nibble words describe 32 copies of one state.
	if b != nil && b.bits == 4 {
		j := (i &^ 31) >> 4
		lo, hi := b.words[j], b.words[j+1]
		if lo == hi && lo == (lo&15)*0x1111111111111111 {
			bits := spawnCollisionBits(uint16(b.palette[lo&15]))
			mergeSpawnCells(word, bits*0x5555555555555555)
			return bits
		}
	}
	state := sec.fill
	if b != nil {
		state = b.get(i)
	}
	bits := spawnCollisionBits(state)
	mergeSpawnCells(word, bits<<shift)
	return bits
}

// mergeSpawnCells ORs cells into a word without losing concurrent fills; a
// cell both sides hold agrees, since storage cannot change under the read lock.
func mergeSpawnCells(word *atomic.Uint64, cells uint64) {
	for {
		old := word.Load()
		if old|cells == old || word.CompareAndSwap(old, old|cells) {
			return
		}
	}
}

// Only the class is static; a live entry still resolves the lid at the cell.
var spawnCollisionKinds = func() (kinds [65536]uint8) {
	for state := range kinds {
		switch {
		case registry.IsShulkerBoxState(uint16(state)):
			kinds[state] = 3
		case registry.IsRedstoneConductorState(uint16(state)):
			kinds[state] = 2
		default:
			kinds[state] = 1
		}
	}
	return kinds
}()

func spawnCollisionBits(state uint16) uint64 { return uint64(spawnCollisionKinds[state]) }
