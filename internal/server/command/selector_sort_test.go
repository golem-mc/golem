package command

import (
	"testing"

	"github.com/golem-mc/golem/internal/commandsyntax"
	"github.com/golem-mc/golem/internal/server/player"
)

type sortPositionEntity struct {
	Entity
	pos   player.Position
	reads int
}

func (e *sortPositionEntity) Position() player.Position {
	e.reads++
	return e.pos
}

func TestSelectorSortCachesDistanceAndKeepsTies(t *testing.T) {
	for _, tc := range []struct {
		name  string
		order int
		want  []int
	}{
		{"nearest", commandsyntax.OrderNearest, []int{1, 3, 4, 0, 2, 5}},
		{"furthest", commandsyntax.OrderFurthest, []int{5, 0, 2, 3, 4, 1}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, limit := range []int{1, 4, 6, 10} {
				bodies := []*sortPositionEntity{
					{pos: player.Position{X: 6, Y: 8, Z: 5}},
					{pos: player.Position{X: 3, Y: 4, Z: 5}},
					{pos: player.Position{X: 3, Y: 4, Z: 10}},
					{pos: player.Position{X: 3, Y: 1, Z: 5}},
					{pos: player.Position{X: 0, Y: 4, Z: 5}},
					{pos: player.Position{X: 3, Y: 4, Z: -5}},
				}
				entities := make([]Entity, len(bodies))
				for i, e := range bodies {
					entities[i] = e
				}
				sel := &Selector{Order: tc.order, MaxResults: limit}
				got := sel.sortAndLimit(nil, player.Position{X: 3, Y: 4, Z: 5}, entities)
				if len(got) != min(limit, len(bodies)) || cap(got) != len(got) {
					t.Fatalf("limit %d: result len/cap = %d/%d", limit, len(got), cap(got))
				}
				for i, e := range got {
					if e != bodies[tc.want[i]] {
						t.Errorf("limit %d: result %d = %v, want body %d", limit, i, e, tc.want[i])
					}
				}
				for i, e := range bodies {
					if e.reads != 1 {
						t.Errorf("limit %d: body %d position read %d times, want 1", limit, i, e.reads)
					}
				}
			}
		})
	}
}
