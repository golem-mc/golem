package entity

import (
	"iter"

	"github.com/golem-mc/golem/internal/server/world"
	"github.com/google/uuid"
)

// Filing is a body's entry in a ChunkIndex; the zero value is unfiled.
type Filing struct {
	ix    filer
	chunk world.ChunkPos
	slot  int32
	id    uuid.UUID
}

type filer interface {
	refile(f *Filing, to world.ChunkPos)
	remove(f *Filing)
}

// Refile moves the body to another chunk of its index. It does nothing when
// the body is unfiled or the chunk is unchanged.
func (f *Filing) Refile(to world.ChunkPos) {
	if f.ix != nil && f.chunk != to {
		f.ix.refile(f, to)
	}
}

// Unfile takes the body out of its index. It does nothing when unfiled.
func (f *Filing) Unfile() {
	if f.ix != nil {
		f.ix.remove(f)
	}
}

// Place is a body's position. Write X and Z only through SetPos: the chunk
// index depends on it.
type Place struct {
	X, Y, Z float64
	Filing
}

// SetPos moves the body and re-files it when it crossed a chunk border.
func (p *Place) SetPos(x, y, z float64) {
	p.X, p.Y, p.Z = x, y, z
	p.Refile(ChunkAt(x, z))
}

// InChunk reports whether the body's X and Z are in the chunk.
func (p *Place) InChunk(pos world.ChunkPos) bool { return ChunkAt(p.X, p.Z) == pos }

// ChunkAt is the chunk holding the horizontal position.
func ChunkAt(x, z float64) world.ChunkPos {
	return world.ChunkPos{X: world.ChunkOf(x), Z: world.ChunkOf(z)}
}

// ChunkIndex files bodies by chunk and UUID. The lock guarding the bodies'
// list guards it. The zero value is ready to use.
type ChunkIndex[T any] struct {
	cells   map[world.ChunkPos][]chunkEntry[T]
	byID    map[uuid.UUID]*Filing
	regions map[world.ChunkPos]int // occupied chunks per region, when enabled
}

// NewRegionalChunkIndex allows rectangle queries to skip empty regions.
func NewRegionalChunkIndex[T any]() ChunkIndex[T] {
	return ChunkIndex[T]{regions: make(map[world.ChunkPos]int)}
}

const regionChunkShift = 5 // 32 chunks along each axis, the width of a region file

func regionOf(pos world.ChunkPos) world.ChunkPos {
	return world.ChunkPos{X: pos.X >> regionChunkShift, Z: pos.Z >> regionChunkShift}
}

type chunkEntry[T any] struct {
	body T
	f    *Filing
}

// Add files body under pos and id. A body already in ix is moved; one in
// another index leaves it first.
func (ix *ChunkIndex[T]) Add(body T, f *Filing, pos world.ChunkPos, id uuid.UUID) {
	if f.ix != nil {
		if f.ix == filer(ix) && f.id == id && ix.owns(f) {
			f.Refile(pos)
			return
		}
		f.Unfile()
	}
	ix.insert(body, f, pos, id)
}

func (ix *ChunkIndex[T]) insert(body T, f *Filing, pos world.ChunkPos, id uuid.UUID) {
	if ix.cells == nil {
		ix.cells = make(map[world.ChunkPos][]chunkEntry[T])
		ix.byID = make(map[uuid.UUID]*Filing)
	}
	f.ix, f.id = ix, id
	ix.byID[id] = f
	ix.place(body, f, pos)
}

func (ix *ChunkIndex[T]) place(body T, f *Filing, pos world.ChunkPos) {
	cell := ix.cells[pos]
	if ix.regions != nil && len(cell) == 0 {
		ix.regions[regionOf(pos)]++
	}
	f.chunk, f.slot = pos, int32(len(cell))
	ix.cells[pos] = append(cell, chunkEntry[T]{body, f})
}

// owns reports whether f's own entry is in ix. A struct copy carries the
// original's Filing, which points at an entry that is not the copy's.
func (ix *ChunkIndex[T]) owns(f *Filing) bool {
	cell := ix.cells[f.chunk]
	return f.slot >= 0 && int(f.slot) < len(cell) && cell[f.slot].f == f
}

func (ix *ChunkIndex[T]) refile(f *Filing, to world.ChunkPos) {
	if !ix.owns(f) {
		*f = Filing{}
		return
	}
	body := ix.detach(f)
	ix.place(body, f, to)
}

// remove takes f's entry out in constant time.
func (ix *ChunkIndex[T]) remove(f *Filing) {
	if !ix.owns(f) {
		*f = Filing{}
		return
	}
	if ix.byID[f.id] == f {
		delete(ix.byID, f.id)
	}
	ix.detach(f)
	*f = Filing{}
}

// detach releases f's slot while leaving its UUID entry in place.
func (ix *ChunkIndex[T]) detach(f *Filing) T {
	cell := ix.cells[f.chunk]
	body := cell[f.slot].body
	last := len(cell) - 1
	moved := cell[last]
	cell[f.slot] = moved
	moved.f.slot = f.slot
	cell[last] = chunkEntry[T]{}
	if last == 0 {
		delete(ix.cells, f.chunk)
		if ix.regions != nil {
			region := regionOf(f.chunk)
			if n := ix.regions[region]; n > 1 {
				ix.regions[region] = n - 1
			} else {
				delete(ix.regions, region)
			}
		}
	} else {
		ix.cells[f.chunk] = cell[:last]
	}
	return body
}

// ByUUID finds the body filed under id.
func (ix *ChunkIndex[T]) ByUUID(id uuid.UUID) (T, bool) {
	if f := ix.byID[id]; f != nil && ix.owns(f) {
		return ix.cells[f.chunk][f.slot].body, true
	}
	var zero T
	return zero, false
}

// In yields the bodies filed under the chunk. The cell is live: move or
// remove nothing while ranging, and do not keep it past the lock.
func (ix *ChunkIndex[T]) In(pos world.ChunkPos) iter.Seq[T] {
	return func(yield func(T) bool) {
		for _, e := range ix.cells[pos] {
			if !yield(e.body) {
				return
			}
		}
	}
}

// ChunksIn yields chunks in the inclusive rectangle, skipping empty regions
// when the index tracks them. The caller holds the index's lock.
func (ix *ChunkIndex[T]) ChunksIn(from, to world.ChunkPos) iter.Seq[world.ChunkPos] {
	return func(yield func(world.ChunkPos) bool) {
		first, last := regionOf(from), regionOf(to)
		for rx := first.X; rx <= last.X; rx++ {
			for rz := first.Z; rz <= last.Z; rz++ {
				if ix.regions != nil && ix.regions[world.ChunkPos{X: rx, Z: rz}] == 0 {
					continue
				}
				x0, x1 := max(from.X, rx<<regionChunkShift), min(to.X, ((rx+1)<<regionChunkShift)-1)
				z0, z1 := max(from.Z, rz<<regionChunkShift), min(to.Z, ((rz+1)<<regionChunkShift)-1)
				for x := x0; x <= x1; x++ {
					for z := z0; z <= z1; z++ {
						if !yield(world.ChunkPos{X: x, Z: z}) {
							return
						}
					}
				}
			}
		}
	}
}

// Has reports whether any body is filed under the chunk.
func (ix *ChunkIndex[T]) Has(pos world.ChunkPos) bool { return len(ix.cells[pos]) != 0 }

// Len is the number of bodies filed.
func (ix *ChunkIndex[T]) Len() int {
	n := 0
	for _, cell := range ix.cells {
		n += len(cell)
	}
	return n
}

// Holds reports whether f is filed in ix under pos.
func (ix *ChunkIndex[T]) Holds(f *Filing, pos world.ChunkPos) bool {
	return f.ix == filer(ix) && f.chunk == pos && ix.owns(f)
}
