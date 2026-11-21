package entity

import "github.com/golem-mc/golem/internal/server/world"

// ChunkIndex files mobs by the chunk of their feet. The herd's lock guards it.
// The zero value is ready to use.
type ChunkIndex struct {
	cells map[world.ChunkPos][]*Mob
}

func feetChunk(m *Mob) world.ChunkPos {
	return world.ChunkPos{X: world.ChunkOf(m.X), Z: world.ChunkOf(m.Z)}
}

// Add files m under its current chunk. It does nothing when m is already
// in ix, and takes m out of any other index first.
func (ix *ChunkIndex) Add(m *Mob) {
	if m.chunks == ix {
		return
	}
	if m.chunks != nil {
		m.chunks.Remove(m)
	}
	if ix.cells == nil {
		ix.cells = make(map[world.ChunkPos][]*Mob)
	}
	m.chunks = ix
	ix.file(m, feetChunk(m))
}

func (ix *ChunkIndex) file(m *Mob, pos world.ChunkPos) {
	cell := ix.cells[pos]
	m.chunk = pos
	m.chunkSlot = int32(len(cell))
	ix.cells[pos] = append(cell, m)
}

// Remove takes m out of ix in constant time.
func (ix *ChunkIndex) Remove(m *Mob) {
	if m.chunks != ix {
		return
	}
	cell := ix.cells[m.chunk]
	last := len(cell) - 1
	moved := cell[last]
	cell[m.chunkSlot] = moved
	moved.chunkSlot = m.chunkSlot
	cell[last] = nil
	if last == 0 {
		delete(ix.cells, m.chunk)
	} else {
		ix.cells[m.chunk] = cell[:last]
	}
	m.chunks = nil
	m.chunkSlot = 0
}

// In returns the mobs whose feet are in the chunk. The caller must not keep
// the slice after releasing the herd's lock.
func (ix *ChunkIndex) In(pos world.ChunkPos) []*Mob { return ix.cells[pos] }

// Resync re-files every member whose chunk changed without going through
// SetPos and returns how many it moved.
func (ix *ChunkIndex) Resync() int {
	var movers []*Mob
	for pos, cell := range ix.cells {
		for _, m := range cell {
			if feetChunk(m) != pos {
				movers = append(movers, m)
			}
		}
	}
	for _, m := range movers {
		ix.Remove(m)
		ix.Add(m)
	}
	return len(movers)
}

// SetPos moves m. Every write to X or Z goes through it, because the chunk
// index depends on it.
func (m *Mob) SetPos(x, y, z float64) {
	m.X, m.Y, m.Z = x, y, z
	if m.chunks != nil {
		if pos := feetChunk(m); pos != m.chunk {
			ix := m.chunks
			ix.Remove(m)
			ix.Add(m)
		}
	}
}

// InChunk reports whether the mob's feet are in the chunk.
func (m *Mob) InChunk(pos world.ChunkPos) bool { return feetChunk(m) == pos }
