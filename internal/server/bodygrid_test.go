package server

import (
	"slices"
	"testing"
)

// TestBodyGridNearFollowsMovesInIndexOrder files bodies, moves some across
// cells, and checks a query sees exactly the bodies filed in the cells its
// box reaches, sorted by index.
func TestBodyGridNearFollowsMovesInIndexOrder(t *testing.T) {
	g := takeBodyGrid()
	defer g.release()
	pos := [][2]float64{{0.5, 0.5}, {10, 10}, {1.5, 1.9}, {-0.5, 0.5}, {3, 0.5}}
	for i, p := range pos {
		g.add(i, p[0], p[1])
	}
	if got := g.near(0, 0, 1.9, 1.9, nil); !slices.Equal(got, []int{0, 2}) {
		t.Errorf("near the first cell = %v, want [0 2]", got)
	}
	// 0 leaves the cell for 1's, 1 takes 0's place; 2's slot is swapped.
	g.moved(0, 11, 11)
	g.moved(1, 0.2, 0.2)
	g.moved(2, 1.2, 1.2) // the same cell: no re-file
	if got := g.near(0, 0, 1.9, 1.9, nil); !slices.Equal(got, []int{1, 2}) {
		t.Errorf("near the first cell after the moves = %v, want [1 2]", got)
	}
	if got := g.near(-1, 0, 2.5, 1, []int{9}); !slices.Equal(got, []int{9, 1, 2, 3, 4}) {
		t.Errorf("near three cells = %v, want [9 1 2 3 4]", got)
	}
	if got := g.near(10, 10, 11, 11, nil); !slices.Equal(got, []int{0}) {
		t.Errorf("near the far cell = %v, want [0]", got)
	}
}
