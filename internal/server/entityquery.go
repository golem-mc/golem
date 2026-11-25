package server

import (
	"iter"
	"math"
	"sync"

	"github.com/golem-mc/golem/internal/server/command"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/world"
)

// entityQuery collects identities through the same filters as full snapshots.
type entityQuery struct {
	*command.EntityQuery
	candidates *[]entityCandidate
	storage    *[]entityCandidate
	// A small first batch avoids bounds, chunk and tag work for higher IDs.
	// If it cannot fill the result, the remainder is collected in one pass.
	batch int
	worst int
	more  bool
	// Exclusive int64 bounds can contain the complete int32 ID range.
	before, after int64
	kind          *entityQueryKind
	// chunk, when set, keeps only bodies whose feet are in that chunk.
	chunk *world.ChunkPos
	// area narrows an overlap query to chunks that can contain matches.
	area *chunkArea
}

type chunkArea struct{ minX, minZ, maxX, maxZ int32 }

func (a chunkArea) each(yield func(world.ChunkPos) bool) {
	for z := a.minZ; z <= a.maxZ; z++ {
		for x := a.minX; x <= a.maxX; x++ {
			if !yield(world.ChunkPos{X: x, Z: z}) {
				return
			}
		}
	}
}

func smallChunkArea(b *[6]float64) *chunkArea {
	a, ok := chunkAreaOf(b)
	if !ok {
		return nil
	}
	width, depth := int64(a.maxX-a.minX+1), int64(a.maxZ-a.minZ+1)
	if width*depth > 256 {
		return nil
	}
	return &a
}

func chunkAreaOf(b *[6]float64) (chunkArea, bool) {
	// Bodies filed across a border can still overlap the box.
	// vanilla: EntitySectionStorage.forEachAccessibleNonEmptySection
	const padding = 2.0
	minX, minZ, maxX, maxZ := b[0]-padding, b[2]-padding, b[3]+padding, b[5]+padding
	// ChunkOf floors to int32 block coordinates. Keep unbounded or
	// unrepresentable plugin boxes on the list path.
	if !(math.MinInt32 <= minX && minX <= maxX && maxX < math.MaxInt32+1 &&
		math.MinInt32 <= minZ && minZ <= maxZ && maxZ < math.MaxInt32+1) {
		return chunkArea{}, false
	}
	return chunkArea{world.ChunkOf(minX), world.ChunkOf(minZ), world.ChunkOf(maxX), world.ChunkOf(maxZ)}, true
}

func (a chunkArea) contains(pos world.ChunkPos) bool {
	return pos.X >= a.minX && pos.X <= a.maxX && pos.Z >= a.minZ && pos.Z <= a.maxZ
}

func bodiesInChunks[E any](ix *entity.ChunkIndex[E], q *entityQuery) iter.Seq[E] {
	if q.chunk != nil {
		return ix.In(*q.chunk)
	}
	return func(yield func(E) bool) {
		for pos := range q.area.each {
			for body := range ix.In(pos) {
				if !yield(body) {
					return
				}
			}
		}
	}
}

func newEntityQuery(filters *command.EntityQuery) entityQuery {
	return entityQuery{EntityQuery: filters, before: math.MaxInt32 + 1, after: math.MinInt32 - 1}
}

func (q *entityQuery) filters() *command.EntityQuery {
	if q == nil {
		return nil
	}
	return q.EntityQuery
}

type entityCandidate struct {
	id   int32
	ref  any
	kind *entityQueryKind // nil for a player, which is already a command.Entity
}

type entityQueryKind struct {
	mu       *sync.Mutex
	snapshot func(any, *entityQuery) *entitySnapshot
}

var entityCandidates = sync.Pool{New: func() any { return new([]entityCandidate) }}

func (q *entityQuery) skipCandidate(id int32) bool {
	if int64(id) <= q.after {
		return true
	}
	if int64(id) >= q.before {
		q.more = true
		return true
	}
	return false
}

func (q *entityQuery) collect(candidate entityCandidate) {
	list := *q.candidates
	if q.batch > 0 && len(list) == q.batch {
		list[q.worst] = candidate
		q.more = true
	} else {
		list = append(list, candidate)
	}
	*q.candidates = list
	if q.batch > 0 {
		q.worst = 0
		for i := range list {
			if list[i].id > list[q.worst].id {
				q.worst = i
			}
		}
		if len(list) == q.batch {
			q.before = int64(list[q.worst].id)
		}
	}
}
