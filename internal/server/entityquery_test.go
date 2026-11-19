package server

import (
	"cmp"
	"math"
	"slices"
	"sync"
	"testing"

	"github.com/golem-mc/golem/internal/server/command"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/registry"
	"github.com/google/uuid"
)

func TestEntitySnapshotsRejectBeforeBounds(t *testing.T) {
	// No world or pose data: reaching the bounds or chunk check would panic.
	s := &Server{}
	for _, tc := range []struct {
		name string
		snap func(*entityQuery) *entitySnapshot
	}{
		{"mob", func(q *entityQuery) *entitySnapshot {
			return s.mobSnapshot(&mobEntity{mob: &entity.Mob{Type: entity.TypeCow}}, "", q)
		}},
		{"item", func(q *entityQuery) *entitySnapshot { return s.itemSnapshot(&itemEntity{}, q) }},
		{"orb", func(q *entityQuery) *entitySnapshot { return s.orbSnapshot(&orbEntity{}, q) }},
		{"arrow", func(q *entityQuery) *entitySnapshot {
			return s.arrowSnapshot(&arrowEntity{arrow: &entity.Arrow{}}, q)
		}},
		{"thrown", func(q *entityQuery) *entitySnapshot {
			return s.thrownSnapshot(&thrownEntity{throwable: &entity.Thrown{}}, q)
		}},
		{"tnt", func(q *entityQuery) *entitySnapshot { return s.tntSnapshot(&tntEntity{}, q) }},
		{"boat", func(q *entityQuery) *entitySnapshot {
			return s.boatSnapshot(&boatEntity{kind: registry.EntityTypeOakBoat}, q)
		}},
		{"minecart", func(q *entityQuery) *entitySnapshot {
			return s.minecartSnapshot(&minecartEntity{kind: registry.EntityTypeMinecart}, q)
		}},
		{"armor stand", func(q *entityQuery) *entitySnapshot { return s.armorStandSnapshot(&armorStandEntity{}, q) }},
		{"mannequin", func(q *entityQuery) *entitySnapshot { return s.mannequinSnapshot(&mannequinEntity{}, q) }},
		{"end crystal", func(q *entityQuery) *entitySnapshot { return s.endCrystalSnapshot(&endCrystalEntity{}, q) }},
		{"cloud", func(q *entityQuery) *entitySnapshot { return s.cloudSnapshot(&cloudEntity{}, q) }},
		{"fangs", func(q *entityQuery) *entitySnapshot { return s.fangsSnapshot(&fangsEntity{}, q) }},
		{"eye", func(q *entityQuery) *entitySnapshot { return s.eyeSnapshot(&eyeEntity{}, q) }},
		{"bolt", func(q *entityQuery) *entitySnapshot { return s.boltSnapshot(&boltEntity{}, q) }},
		{"firework", func(q *entityQuery) *entitySnapshot { return s.fireworkSnapshot(&fireworkEntity{}, q) }},
		{"bobber", func(q *entityQuery) *entitySnapshot { return s.bobberSnapshot(&bobberEntity{}, q) }},
		{"knot", func(q *entityQuery) *entitySnapshot { return s.knotSnapshot(&knotEntity{}, q) }},
		{"painting", func(q *entityQuery) *entitySnapshot { return s.paintingSnapshot(&paintingEntity{}, q) }},
		{"frame", func(q *entityQuery) *entitySnapshot { return s.frameSnapshot(&frameEntity{}, q) }},
		{"glow frame", func(q *entityQuery) *entitySnapshot { return s.frameSnapshot(&frameEntity{glow: true}, q) }},
		{"fixture", func(q *entityQuery) *entitySnapshot {
			return s.fixtureSnapshot(&fixtureEntity{kind: entity.FixtureMarker}, q)
		}},
		{"falling", func(q *entityQuery) *entitySnapshot { return s.fallingSnapshot(&fallingEntity{}, q) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, q := range []command.EntityQuery{
				{Type: "minecraft:player"},
				{UUID: new(uuid.New())},
				{Tags: []command.TagFilter{{Name: "missing"}}},
			} {
				if got := tc.snap(&entityQuery{EntityQuery: &q}); got != nil {
					t.Fatalf("query %+v returned a snapshot", q)
				}
			}
		})
	}
}

func TestAppendSnapshotsStopsAfterUUIDSnapshot(t *testing.T) {
	id := uuid.New()
	q := entityQuery{EntityQuery: &command.EntityQuery{UUID: &id}}
	for _, accepted := range []bool{false, true} {
		var mu sync.Mutex
		list := []int{0, 1, 2}
		visits, matches := 0, 0
		out := snapshotAppender(nil, &q, &mu, &list, func(i int) int32 { return int32(i) },
			func(_ *Server, i int, query *entityQuery) *entitySnapshot {
				visits++
				if query != &q {
					t.Fatal("snapshot did not receive the bound query")
				}
				if i != 1 {
					return nil
				}
				matches++
				if !accepted {
					// A UUID match can still fail the snapshot's visibility check.
					return nil
				}
				return &entitySnapshot{uuid: id}
			})(nil)
		wantVisits, wantResults := 3, 0
		if accepted {
			wantVisits, wantResults = 2, 1
		}
		if visits != wantVisits || matches != 1 || len(out) != wantResults {
			t.Fatalf("accepted = %v: visits = %d, matches = %d, results = %d; want %d, 1, %d", accepted, visits, matches, len(out), wantVisits, wantResults)
		}
		if !mu.TryLock() {
			t.Fatal("query retained the kind's lock")
		}
		mu.Unlock()
	}
}

func TestEntityQueryCandidateBatchKeepsLowestIDs(t *testing.T) {
	for _, descending := range []bool{false, true} {
		var candidates []entityCandidate
		q := newEntityQuery(nil)
		q.candidates, q.batch = &candidates, 16
		for i := range 80 {
			id := int32(i*37%80 - 40)
			if descending {
				id = int32(39 - i)
			}
			if q.skipCandidate(id) || id%2 == 0 {
				continue
			}
			q.collect(entityCandidate{id: id})
		}
		slices.SortFunc(candidates, func(a, b entityCandidate) int { return cmp.Compare(a.id, b.id) })
		if len(candidates) != 16 || !q.more {
			t.Fatalf("batch = %v, more = %v", candidates, q.more)
		}
		for i, candidate := range candidates {
			if want := int32(-39 + 2*i); candidate.id != want {
				t.Fatalf("candidate %d = %d, want %d", i, candidate.id, want)
			}
		}
		q.after, q.before = int64(candidates[len(candidates)-1].id), math.MaxInt32+1
		q.batch, q.more = 0, false
		candidates = candidates[:0]
		for i := range 80 {
			id := int32(i - 40)
			if !q.skipCandidate(id) && id%2 != 0 {
				q.collect(entityCandidate{id: id})
			}
		}
		if len(candidates) != 24 || candidates[0].id != -7 || candidates[23].id != 39 || q.more {
			t.Fatalf("remaining candidates = %v, more = %v", candidates, q.more)
		}
	}
}
