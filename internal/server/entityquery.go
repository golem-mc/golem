package server

import (
	"math"
	"sync"

	"github.com/golem-mc/golem/internal/server/command"
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
