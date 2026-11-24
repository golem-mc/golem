package server

import (
	"slices"
	"testing"

	"github.com/golem-mc/golem/internal/server/entity"
)

func TestResetItemNeighboursClearsShortenedQuery(t *testing.T) {
	t.Parallel()
	n := newBodyNear(entity.ItemMergeReach, func(it *entity.Item) (float64, float64) { return it.X, it.Z })
	group := make([]entity.Item, 32)
	for i := range group {
		n.add(&group[i])
	}
	lone := &entity.Item{X: 8.5}
	n.add(lone)
	if got := len(n.query(&group[0])); got != len(group) {
		t.Fatalf("large query returned %d candidates, want %d", got, len(group))
	}
	if got := n.query(lone); !slices.Equal(got, []*entity.Item{lone}) {
		t.Fatalf("small query returned %v, want only the distant item", got)
	}
	bodies, out := n.bodies[:cap(n.bodies)], n.out[:cap(n.out)]
	n.reset()
	if n.grid != nil || len(n.bodies) != 0 || len(n.idx) != 0 || len(n.out) != 0 {
		t.Fatal("reset left a grid or nonempty slice")
	}
	for _, buf := range [][]*entity.Item{bodies, out} {
		for i, it := range buf {
			if it != nil {
				t.Fatalf("pointer slot %d retained an item after reset", i)
			}
		}
	}
	first, second := &entity.Item{X: 2.2}, &entity.Item{X: 1.9}
	if a, b := n.add(first), n.add(second); a != 0 || b != 1 {
		t.Fatalf("indices after reset are %d and %d, want 0 and 1", a, b)
	}
	if got := n.query(second); !slices.Equal(got, []*entity.Item{first, second}) {
		t.Errorf("query after reset returned %v, want the two new items in registration order", got)
	}
	n.reset()
}
